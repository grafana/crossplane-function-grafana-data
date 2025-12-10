package main

import (
	"context"
	"fmt"

	"github.com/grafana/crossplane-function-grafana-data/pkg/clients"

	"github.com/crossplane/crossplane-runtime/v2/pkg/fieldpath"
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
//
//nolint:gocyclo // ignore
func (f *Function) RunFunction(_ context.Context, req *fnv1.RunFunctionRequest) (*fnv1.RunFunctionResponse, error) {
	f.log.Info("Running function", "grafana-data", req.GetMeta().GetTag())

	clientMap := make(map[string]*clients.Client)

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

		if _, ok := clientMap[providerConfigName]; !ok {
			cf := clientsFetcher{
				req:                req,
				rsp:                rsp,
				providerConfigName: providerConfigName,
			}
			cs, err := cf.getClients()
			if err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}
			if cs == nil {
				// grabbing the providerConfig and secret for setting up the clients might need a few roundtrips
				continue
			}
			clientMap[providerConfigName] = cs
		}

		switch gvk.Group {
		case "oncall.grafana.crossplane.io":
			if err := NewOnCallClient(clientMap[providerConfigName].OnCallClient).Process(desired); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "sm.grafana.crossplane.io":
			if err := NewSMClient(clientMap[providerConfigName].SMAPI).Process(desired); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}

		case "oss.grafana.crossplane.io":
			if err := NewGrafanaClient(clientMap[providerConfigName].GrafanaAPI).Process(desired); err != nil {
				response.Fatal(rsp, err)
				return rsp, nil
			}
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

func replacePath[V, W any](desired *resource.DesiredComposed, path string, fn func(V) (W, error)) error {
	var val V
	if err := fieldpath.Pave(desired.Resource.Object).GetValueInto(path, &val); err != nil {
		//nolint:nilerr // simply return if no value found at path
		return nil
	}

	newVal, err := fn(val)
	if err != nil {
		// TODO: replace with proper response function
		fmt.Println(err)
	}

	if err := desired.Resource.SetValue(path, newVal); err != nil {
		gvk := desired.Resource.GroupVersionKind()
		return errors.Wrapf(err, "cannot set value for %s", gvk.Kind)
	}

	return nil
}
