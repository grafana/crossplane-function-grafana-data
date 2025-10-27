package main

import (
	"context"
	"fmt"

	"github.com/grafana/crossplane-function-grafana-data/pkg/clients"
	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/crossplane-runtime/pkg/fieldpath"
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

	Clients map[string]*clients.Client
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "grafana-data", req.GetMeta().GetTag())

	f.Clients = make(map[string]*clients.Client)

	rsp := response.To(req, response.DefaultTTL)

	// The composed resources desired by any previous Functions in the pipeline.
	desiredComposed, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired composed resources from %T", req))
		return rsp, nil
	}

	for _, desired := range desiredComposed {
		gvk := desired.Resource.GroupVersionKind()

		var providerConfigName string
		if err := fieldpath.Pave(desired.Resource.Object).GetValueInto("spec.providerConfigRef.name", &providerConfigName); err != nil {
			// return if no value found at path
			response.Fatal(rsp, errors.Wrapf(err, "cannot find providerConfig for resource %T", desired))
			return rsp, nil
		}

		cont, err := f.getClients(providerConfigName, rsp, req)
		if err != nil {
			response.Fatal(rsp, err)
			return rsp, nil
		}
		if cont {
			// grabbing the providerConfig and secret for setting up the clients might need a few roundtrips
			continue
		}

		switch gvk.Group {
		case "oncall.grafana.crossplane.io":
			if err := NewOnCallClient(f.Clients[providerConfigName].OnCallClient).Process(desired); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		// dummy to stop linter complaining
		case "xxx.grafana.crossplane.io":
			continue
		}
	}

	if err := response.SetDesiredComposedResources(rsp, desiredComposed); err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot set desired composed resources in %T", rsp))
		return rsp, nil
	}

	response.Normalf(rsp, "Successfully Processed")
	response.ConditionTrue(rsp, "FunctionSuccess", "Success").TargetCompositeAndClaim()
	return rsp, nil
}

func (f *Function) getClients(providerConfigName string, rsp *fnv1.RunFunctionResponse, req *fnv1.RunFunctionRequest) (bool, error) {
	if _, ok := f.Clients[providerConfigName]; ok {
		return false, nil
	}

	providerConfig, secret, err := getProviderConfig(rsp, req, providerConfigName)
	if err != nil {
		return false, errors.Wrap(err, "Could not get providerConfig or secret")
	}
	if providerConfig == nil || secret == nil {
		return true, nil
	}

	cs, err := clients.NewClientsFromProviderConfig(providerConfig, secret, "instanceCredentials")
	if err != nil {
		return false, err
	}

	f.Clients[providerConfigName] = cs
	return false, nil
}

func replacePath[V any](desired *resource.DesiredComposed, path string, fn func(V) V) error {
	var val V
	if err := fieldpath.Pave(desired.Resource.Object).GetValueInto(path, &val); err != nil {
		//nolint:nilerr // simply return if no value found at path
		return nil
	}

	newVal := fn(val)

	if err := desired.Resource.SetValue(path, newVal); err != nil {
		gvk := desired.Resource.GroupVersionKind()
		return errors.Wrapf(err, "cannot set value for %s", gvk.Kind)
	}

	return nil
}

func getProviderConfig(rsp *fnv1.RunFunctionResponse, req *fnv1.RunFunctionRequest, providerConfigName string) (*v1beta1.ProviderConfig, *v1.Secret, error) {
	providerConfig, err := getRequiredResource[v1beta1.ProviderConfig](rsp, req,
		&fnv1.ResourceSelector{
			ApiVersion: "grafana.crossplane.io/v1beta1",
			Kind:       "ProviderConfig",
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: providerConfigName,
			},
		},
	)

	if providerConfig == nil || err != nil {
		return nil, nil, err
	}

	secret, err := getRequiredResource[v1.Secret](rsp, req,
		&fnv1.ResourceSelector{
			ApiVersion: "v1",
			Kind:       "Secret",
			Namespace:  &providerConfig.Spec.Credentials.SecretRef.Namespace,
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: providerConfig.Spec.Credentials.SecretRef.Name,
			},
		},
	)
	if secret == nil || err != nil {
		return nil, nil, err
	}
	return providerConfig, secret, nil
}

func getRequiredResource[R any](rsp *fnv1.RunFunctionResponse, req *fnv1.RunFunctionRequest, selector *fnv1.ResourceSelector) (*R, error) {
	key := fmt.Sprintf("%s/%s", selector.GetKind(), selector.GetMatchName())

	if rsp.GetRequirements() == nil {
		rsp.Requirements = &fnv1.Requirements{}
	}
	if rsp.GetRequirements().GetResources() == nil {
		rsp.Requirements.Resources = make(map[string]*fnv1.ResourceSelector)
	}
	rsp.Requirements.Resources[key] = selector

	requiredResources, err := request.GetRequiredResources(req)
	if err != nil {
		return nil, errors.Wrapf(err, "cannot get requiredResources resources with secret")
	}
	rr, ok := requiredResources[key]
	if !ok {
		return nil, nil
	}

	if len(rr) > 1 {
		return nil, errors.Errorf("Too many resources returned")
	}

	var rs R
	if err = runtime.DefaultUnstructuredConverter.
		FromUnstructured(rr[0].Resource.Object, &rs); err != nil {
		return nil, errors.Wrapf(err, "cannot convert Secret")
	}
	return &rs, nil
}
