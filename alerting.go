package main

import (
	"strings"

	onCallAPI "github.com/grafana/amixr-api-go-client"
	"github.com/grafana/crossplane-provider-grafana/v2/apis/cluster/alerting/v1alpha1"
	"github.com/grafana/grafana-openapi-client-go/client"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/resource"
)

// AlertingClient is a client with convenience methods
type AlertingClient struct {
	Client       *client.GrafanaHTTPAPI
	OnCallClient *onCallAPI.Client
}

// NewAlertingClient returns a client with convenience methods for Grafana and OnCall
func NewAlertingClient(grafanaClient *client.GrafanaHTTPAPI, oncallClient *onCallAPI.Client) *AlertingClient {
	return &AlertingClient{
		Client:       grafanaClient,
		OnCallClient: oncallClient,
	}
}

// Process processes fields of different kinds
func (c *AlertingClient) Process(desired *resource.DesiredComposed) error {
	gvk := desired.Resource.GroupVersionKind()
	// switch gvk.Kind {
	// case "ContactPoint":
	if gvk.Kind == "ContactPoint" {
		path := "spec.forProvider.oncall"
		return replacePath(desired, path, c.GetOnCallURLs)
	}
	return nil
}

// GetOnCallURLs looks up OnCall integrations and returns the URLs
func (c *AlertingClient) GetOnCallURLs(oncall []v1alpha1.OncallParameters) ([]v1alpha1.OncallParameters, error) {
	newVal := make([]v1alpha1.OncallParameters, 0)
	for _, params := range oncall {
		if strings.HasPrefix(*params.URL, "http://") || strings.HasPrefix(*params.URL, "https://") {
			newVal = append(newVal, params)
			continue
		}

		url, err := c.GetOnCallURL(*params.URL)
		if err != nil {
			return nil, err
		}
		params.URL = &url
		newVal = append(newVal, params)
	}
	return newVal, nil
}

// GetOnCallURL looks up an OnCall integration by name and returns its URL
func (c *AlertingClient) GetOnCallURL(name string) (string, error) {
	page := 1
	for {
		options := &onCallAPI.ListIntegrationOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		response, _, err := c.OnCallClient.Integrations.ListIntegrations(options)
		if err != nil {
			return "", errors.Wrapf(err, "Failed to list oncall integrations")
		}

		// Look for the integration by name
		for _, integration := range response.Integrations {
			if integration.Name == name {
				return integration.Link, nil
			}
		}

		// If no more pages, break
		if response.Next == nil {
			break
		}
		page++
	}

	return "", errors.Errorf("Could not find oncall integration with name %s", name)
}
