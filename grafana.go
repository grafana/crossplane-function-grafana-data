package main

import (
	"fmt"
	"strconv"

	"github.com/go-openapi/runtime"
	"github.com/grafana/crossplane-provider-grafana/apis/oss/v1alpha1"
	"github.com/grafana/grafana-openapi-client-go/client"
	"github.com/grafana/grafana-openapi-client-go/client/teams"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/resource"
)

// GrafanaClient is a client with convenience methods
type GrafanaClient struct {
	Client *client.GrafanaHTTPAPI
}

// NewGrafanaClient returns a client with convenience methods
func NewGrafanaClient(client *client.GrafanaHTTPAPI) *GrafanaClient {
	return &GrafanaClient{
		Client: client,
	}
}

// Process processes fields of different kinds
func (c *GrafanaClient) Process(desired *resource.DesiredComposed) error {
	gvk := desired.Resource.GroupVersionKind()
	// switch gvk.Kind {
	// case "FolderPermission":
	if gvk.Kind == "FolderPermission" {
		path := "spec.forProvider.permissions"
		return replacePath(desired, path, c.GetTeamIDForFolderPermissions)
	}
	return nil
}

// GetTeamIDForFolderPermissions will replace TeamID fields with team names to their ID equivalent
func (c *GrafanaClient) GetTeamIDForFolderPermissions(permissions []v1alpha1.FolderPermissionPermissionsParameters) ([]v1alpha1.FolderPermissionPermissionsParameters, error) {
	newPermissions := make([]v1alpha1.=arameters, 0)
	for _, p := range permissions {
		if p.TeamID != nil {
			teamID, err := c.GetTeam(*p.TeamID)
			if err != nil {
				// return nil, err
				fmt.Println(err) // TODO: handle errors better
			}
			p.TeamID = &teamID
		}
		newPermissions = append(newPermissions, p)
	}
	return newPermissions, nil
}

// GetTeam will return the ID for a team name
func (c *GrafanaClient) GetTeam(name string) (string, error) {
	_, err := c.Client.Teams.GetTeamByID(name)
	if err == nil {
		return name, nil
	}
	if err != nil {
		if respErr, ok := err.(runtime.ClientResponseStatus); !ok || !respErr.IsCode(404) {
			return "", err
		}
	}

	respBySearch, err := c.Client.Teams.SearchTeams(
		teams.NewSearchTeamsParams().WithName(&name),
	)
	if err != nil {
		return "", err
	}
	for _, r := range respBySearch.GetPayload().Teams {
		if r.Name == name {
			return strconv.FormatInt(r.ID, 10), nil
		}
	}

	return name, errors.Errorf("Could not find ID for team")
}
