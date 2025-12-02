package main

import (
	"fmt"

	"github.com/grafana/crossplane-function-grafana-data/pkg/clients"
	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/crossplane/function-sdk-go/errors"
	fnv1 "github.com/crossplane/function-sdk-go/proto/v1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
)

type clientsFetcher struct {
	req                *fnv1.RunFunctionRequest
	rsp                *fnv1.RunFunctionResponse
	providerConfigName string
}

func (cf *clientsFetcher) getClients() (*clients.Client, error) {
	providerConfig, secret, err := cf.getProviderConfig()
	if err != nil {
		return nil, errors.Wrap(err, "Could not get providerConfig or secret")
	}
	if providerConfig == nil || secret == nil {
		return nil, nil
	}

	cs, err := clients.NewClientsFromProviderConfig(providerConfig, secret, "instanceCredentials")
	if err != nil {
		return nil, err
	}

	return cs, nil
}

func (cf *clientsFetcher) getProviderConfig() (*v1beta1.ProviderConfig, *v1.Secret, error) {
	providerConfig, err := cf.getRequiredResource(
		&fnv1.ResourceSelector{
			ApiVersion: "grafana.crossplane.io/v1beta1",
			Kind:       "ProviderConfig",
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: cf.providerConfigName,
			},
		},
	)
	if providerConfig == nil || err != nil {
		return nil, nil, err
	}
	pc, err := convertUnstructured[v1beta1.ProviderConfig](providerConfig.Resource.Object)
	if err != nil {
		return nil, nil, err
	}

	secret, err := cf.getRequiredResource(
		&fnv1.ResourceSelector{
			ApiVersion: "v1",
			Kind:       "Secret",
			Namespace:  &pc.Spec.Credentials.SecretRef.Namespace,
			Match: &fnv1.ResourceSelector_MatchName{
				MatchName: pc.Spec.Credentials.SecretRef.Name,
			},
		},
	)
	if secret == nil || err != nil {
		return nil, nil, err
	}
	sc, err := convertUnstructured[v1.Secret](secret.Resource.Object)
	if err != nil {
		return nil, nil, err
	}

	return pc, sc, nil
}

func (cf *clientsFetcher) getRequiredResource(selector *fnv1.ResourceSelector) (*resource.Required, error) {
	key := fmt.Sprintf("%s/%s", selector.GetKind(), selector.GetMatchName())

	if cf.rsp.GetRequirements() == nil {
		cf.rsp.Requirements = &fnv1.Requirements{}
	}
	if cf.rsp.GetRequirements().GetResources() == nil {
		cf.rsp.Requirements.Resources = make(map[string]*fnv1.ResourceSelector)
	}
	cf.rsp.Requirements.Resources[key] = selector

	requiredResources, err := request.GetRequiredResources(cf.req)
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

	return &rr[0], nil
}

func convertUnstructured[R any](object map[string]any) (*R, error) {
	var rs R
	if err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(object, &rs); err != nil {
		return nil, errors.Wrapf(err, "cannot convert Secret")
	}
	return &rs, nil
}
