package main

import (
	"context"
	"slices"

	"github.com/grafana/synthetic-monitoring-agent/pkg/pb/synthetic_monitoring"
	SMAPI "github.com/grafana/synthetic-monitoring-api-go-client"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/resource"
)

// OnCallClient is a client with convenience methods
type SMClient struct {
	Client *SMAPI.Client
	Probes []synthetic_monitoring.Probe
}

// NewOnCallClient returns a client with convenience methods
func NewSMClient(client *SMAPI.Client) *SMClient {
	return &SMClient{
		Client: client,
	}
}

func (c *SMClient) getProbes() error {
	// only populate the list if the list is empty
	if len(c.Probes) != 0 {
		return nil
	}

	ctx := context.Background()
	probes, err := c.Client.ListProbes(ctx)
	if err != nil {
		return err
	}

	c.Probes = probes
	return nil
}

func (c *SMClient) Process(desired *resource.DesiredComposed) error {
	gvk := desired.Resource.GroupVersionKind()
	// switch gvk.Kind {
	// case "Check":
	if gvk.Kind == "Check" {
		path := "spec.forProvider.probes"
		return replacePath(desired, path, c.GetProbes)
	}
	return nil
}

func (c *SMClient) GetProbes(probes []any) ([]int64, error) {
	probeIDs := []int64{}
	for _, probe := range probes {
		probeID, err := c.GetProbeID(probe)
		if err != nil {
			return nil, err
		}
		probeIDs = append(probeIDs, probeID)
	}
	return probeIDs, nil
}

func (c *SMClient) GetProbeID(probe any) (int64, error) {
	if err := c.getProbes(); err != nil {
		return -1, err
	}

	// WARNING: Probe names can't be set directly on the MRs, the `probes` field only accepts `number` while the probe names are `string`. I expect that a Composition will work as the probe names get replaced by numeric IDs before being applied to Kubernetes.
	probeIDx := slices.IndexFunc(c.Probes, func(c synthetic_monitoring.Probe) bool {
		return c.Id == probe || c.Name == probe
	})

	if probeIDx != -1 {
		return c.Probes[probeIDx].Id, nil
	}

	return -1, errors.Errorf("Could not find probe with ID or name: %s", probe)
}
