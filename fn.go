package main

import (
	"context"
	"fmt"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
	//"github.com/crossplane/function-grafana-data/input/v1beta1"
	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/logging"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1.FunctionRunnerServiceServer

	log logging.Logger
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "grafana-data", req.GetMeta().GetTag())

	rsp := response.To(req, response.DefaultTTL)

	oxr, err := request.GetObservedCompositeResource(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get observed composite resource from %T", req))
		return rsp, nil
	}
	fmt.Printf("%+v", oxr.Resource)

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
			path := "spec.forProvider.users"
			if err := replacePath(desired, path, getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

			path = "spec.forProvider.rollingUsers"
			// rollingUsers is an array of arrays with userIDs
			fn := func(val [][]string) [][]string {
				var newVal [][]string
				for _, userIds := range val {
					usernames := getUserIds(userIds)
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
			if err := replacePath(desired, path, getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

			path = "spec.forProvider.personsToNotifyNextEachTime"
			if err := replacePath(desired, path, getUserIds); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "UserNotificationRule":
			path := "spec.forProvider.userId"
			if err := replacePath(desired, path, getUserId); err != nil {
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

func getUserIds(userIds []string) []string {
	var newVal []string
	for _, username := range userIds {
		userId := getUserId(username)
		newVal = append(newVal, userId)
	}
	return newVal
}

func getUserId(username string) string {
	return fmt.Sprintf("changed_%s", username)
}

func getTeamId(name string) string {
	return fmt.Sprintf("changed_%s", name)
}
