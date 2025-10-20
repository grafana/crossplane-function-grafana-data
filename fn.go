package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"

	onCallAPI "github.com/grafana/amixr-api-go-client"
	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.FunctionRunnerServiceServer

	log logging.Logger

	OnCallClient *onCallAPI.Client
	OnCallUsers  []onCallUser
}

type onCallUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func (f *Function) getAllOnCallUsers() error {
	allUsers := []onCallUser{}
	page := 1
	for {
		options := &onCallAPI.ListUserOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		usersResponse, _, err := f.OnCallClient.Users.ListUsers(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to list oncall users")
		}

		for _, user := range usersResponse.Users {
			allUsers = append(allUsers, onCallUser{
				ID:       user.ID,
				Username: user.Username,
				Email:    user.Email,
				Role:     user.Role,
			})
		}

		if usersResponse.PaginatedResponse.Next == nil {
			break
		}
		page++
	}
	return nil
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "grafana-data", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	//in := &v1beta1.Input{}
	//if err := request.GetInput(req, in); err != nil {
	//	response.Fatal(rsp, errors.Errorf("cannot get Function input from %T: %w", req, err))
	//	return rsp, nil
	//}

	//onCallClient, err := onCallAPI.New()
	//if err != nil {
	//	response.Fatal(rsp, errors.Wrapf(err, "cannot set up onCall API client"))
	//	return rsp, nil
	//}

	//f.OnCallClient = onCallClient

	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed composite resource from %T", req))
		return rsp, nil
	}

	f.log.WithValues(
		"xr-d", oxr.Resource.GetAPIVersion(),
		"xr-kind", oxr.Resource.GetKind(),
		"xr-name", oxr.Resource.GetName(),
	)

	// The composed resources desired by any previous Functions in the pipeline.
	desiredComposed, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired composed resources from %T", req))
		return rsp, nil
	}

	for _, desired := range desiredComposed {
		gvk := desired.Resource.GroupVersionKind()

		switch gvk.Kind {
		case "OnCallShift":
			var providerConfigName string
			if err := fieldpath.Pave(desired.Resource.Object).GetValueInto("spec.providerConfigRef.name", &providerConfigName); err != nil {
				// return if no value found at path
				return nil, errors.Wrapf(err, "cannot find providerConfig for resource %T", desired)
			}

			providerConfig, err := f.requireProviderConfig(rsp, req, providerConfigName)
			if err != nil {
				return nil, err
			}
			if providerConfig == nil {
				continue
			}

			path := "spec.forProvider.users"
			if err := replacePath(desired, path, f.getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

			path = "spec.forProvider.rollingUsers"
			// rollingUsers is an array of arrays with userIDs
			fn := func(val [][]string) [][]string {
				var newVal [][]string
				for _, userIds := range val {
					usernames := f.getUserIds(userIds)
					newVal = append(newVal, usernames)
				}
				return newVal
			}
			if err := replacePath(desired, path, fn); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "Escalation":
			path := "spec.forProvider.personsToNotify"
			if err := replacePath(desired, path, f.getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

			path = "spec.forProvider.personsToNotifyNextEachTime"
			if err := replacePath(desired, path, f.getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "UserNotificationRule":
			path := "spec.forProvider.userId"
			if err := replacePath(desired, path, f.getOnCallUserId); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "Schedule":
			path := "spec.forProvider.teamId"
			if err := replacePath(desired, path, getTeamId); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}
		}
	}

	if err := response.SetDesiredComposedResources(rsp, desiredComposed); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composed resources in %T", rsp))
		return rsp, nil
	}

	response.Normalf(rsp, "Successfully Processed OnCallShift")

	return rsp, nil
}

func replacePath[V any](desired *resource.DesiredComposed, path string, fn func(V) V) error {
	var val V
	if err := fieldpath.Pave(desired.Resource.Object).GetValueInto(path, &val); err != nil {
		// return if no value found at path
		return nil
	}

	newVal := fn(val)

	if err := desired.Resource.SetValue(path, newVal); err != nil {
		gvk := desired.Resource.GroupVersionKind()
		return errors.Wrapf(err, "cannot set value for %s", gvk.Kind)
	}

	return nil
}

func (f *Function) getUserIds(userIds []string) []string {
	var newVal []string
	for _, id := range userIds {
		userId := f.getOnCallUserId(id)
		newVal = append(newVal, userId)
	}
	return newVal
}

func (f *Function) getOnCallUserId(id string) string {
	return fmt.Sprintf("changed_%s", id)
	// populate the list if the list is empty
	if len(f.OnCallUsers) == 0 {
		f.getAllOnCallUsers()
	}

	// check if the provided ID exists
	idx := slices.IndexFunc(f.OnCallUsers, func(c onCallUser) bool {
		return c.ID == id
	})
	if idx != -1 {
		return f.OnCallUsers[idx].ID
	}

	// if the provided ID does not exist, try to look up by username or email
	usernameEmailIdx := slices.IndexFunc(f.OnCallUsers, func(c onCallUser) bool {
		return c.Username == id || c.Email == id
	})
	if usernameEmailIdx != -1 {
		return f.OnCallUsers[usernameEmailIdx].ID
	}

	// ID, username or email not found, return as is
	// TODO: consider failing here
	return id
}

func getTeamId(name string) string {
	return fmt.Sprintf("changed_%s", name)
}

//func (f *Function) getCreds(input v1beta1.Input, req *fnv1.RunFunctionRequest, keyName, secretKeyName string) (string, string, error) {
//	// use the go function SDK to retrieve the raw credentials secret
//	creds, err := request.GetCredentials(req, "grafana-oncall-creds")
//	if err != nil {
//		return "", "", err
//	}
//	rawCreds := string(creds.Data["instanceCredentials"])
//
//	// unmarshal instanceCredentials
//
//	accessKeyID := cfg.Section("default").Key(keyName).String()
//	secretAccessKey := cfg.Section("default").Key(secretKeyName).String()
//	return accessKeyID, secretAccessKey, nil
//}

func (f *Function) requireProviderConfig(rsp *fnv1.RunFunctionResponse, req *fnv1.RunFunctionRequest, providerConfigName string) (*v1beta1.ProviderConfig, error) {
	pvConfName := fmt.Sprintf("providerConfig/%s", providerConfigName)

	if rsp.Requirements == nil {
		rsp.Requirements = &fnv1.Requirements{}
	}
	if rsp.Requirements.ExtraResources == nil {
		rsp.Requirements.ExtraResources = make(map[string]*fnv1.ResourceSelector)
	}
	rsp.Requirements.ExtraResources[pvConfName] = &fnv1.ResourceSelector{
		ApiVersion: "grafana.crossplane.io/v1beta1",
		Kind:       "ProviderConfig",
		Match: &fnv1.ResourceSelector_MatchName{
			MatchName: providerConfigName,
		},
	}

	requiredResources, err := request.GetExtraResources(req)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get requiredResources resources with providerConfig")
	}
	pc, ok := requiredResources[pvConfName]
	if !ok {
		f.log.Info(fmt.Sprintf("ProviderConfig not found: %s", providerConfigName))
		return nil, nil
	}

	f.log.Info(fmt.Sprintf("ProviderConfig found: %s", providerConfigName))

	var providerConfig v1beta1.ProviderConfig
	for _, resource := range pc {
		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(resource.Resource.Object, &providerConfig)
		if err != nil {
			return nil, errors.Wrapf(err, "cannot convert providerConfig")
		}
	}
	return &providerConfig, nil
}
