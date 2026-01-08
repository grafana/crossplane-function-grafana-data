package main

import (
	"fmt"
	"strconv"

	"github.com/go-openapi/runtime"
	"github.com/grafana/crossplane-provider-grafana/apis/oss/v1alpha1"
	"github.com/grafana/grafana-openapi-client-go/client"
	"github.com/grafana/grafana-openapi-client-go/client/access_control"
	"github.com/grafana/grafana-openapi-client-go/client/org"
	"github.com/grafana/grafana-openapi-client-go/client/service_accounts"
	"github.com/grafana/grafana-openapi-client-go/client/teams"
	"github.com/grafana/grafana-openapi-client-go/models"

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
	switch gvk.Kind {
	case "FolderPermission":
		path := "spec.forProvider.permissions"
		return replacePath(desired, path, c.GetTeamIDForFolderPermissions)

	case "RoleAssignmentItem":
		path := "spec.forProvider.roleUid"
		err := replacePath(desired, path, c.GetRoleUID)
		if err != nil {
			return err
		}

		path = "spec.forProvider.serviceAccountId"
		err = replacePath(desired, path, c.GetServiceAccount)
		if err != nil {
			return err
		}

		path = "spec.forProvider.userId"
		err = replacePath(desired, path, c.GetUser)
		if err != nil {
			return err
		}

		path = "spec.forProvider.teamId"
		return replacePath(desired, path, c.GetTeam)
	}
	return nil
}

// GetTeamIDForFolderPermissions will replace TeamID fields with team names to their ID equivalent
func (c *GrafanaClient) GetTeamIDForFolderPermissions(permissions []v1alpha1.FolderPermissionPermissionsParameters) ([]v1alpha1.FolderPermissionPermissionsParameters, error) {
	newPermissions := make([]v1alpha1.FolderPermissionPermissionsParameters, 0)
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

// GetUsers looks up users and returns the IDs
func (c *GrafanaClient) GetUsers(names []string) ([]string, error) {
	newVal := []string{}
	for _, id := range names {
		userID, err := c.GetUser(id)
		if err != nil {
			return nil, err
		}
		newVal = append(newVal, userID)
	}
	return newVal, nil
}

// GetTeams looks up teams and returns the IDs
func (c *GrafanaClient) GetTeams(names []string) ([]string, error) {
	newVal := []string{}
	for _, id := range names {
		teamID, err := c.GetTeam(id)
		if err != nil {
			return nil, err
		}
		newVal = append(newVal, teamID)
	}
	return newVal, nil
}

// GetServiceAccounts looks up serviceAccounts and returns the IDs
func (c *GrafanaClient) GetServiceAccounts(names []string) ([]string, error) {
	newVal := []string{}
	for _, id := range names {
		serviceAccountID, err := c.GetServiceAccount(id)
		if err != nil {
			return nil, err
		}
		newVal = append(newVal, serviceAccountID)
	}
	return newVal, nil
}

// GetTeam will return the ID for a team name
func (c *GrafanaClient) GetTeam(name string) (string, error) {
	_, err := c.Client.Teams.GetTeamByID(name)
	if err == nil {
		return name, nil
	}
	if respErr, ok := err.(runtime.ClientResponseStatus); !ok || !respErr.IsCode(404) {
		return "", err
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

	return name, errors.Errorf("Could not find ID for team: %s", name)
}

// GetRoleUID will return the UID for a role name
func (c *GrafanaClient) GetRoleUID(name string) (string, error) {
	includeHidden := true
	resp, err := c.Client.AccessControl.ListRoles(access_control.NewListRolesParams().WithIncludeHidden(&includeHidden))
	if err != nil {
		return name, err
	}

	for _, r := range resp.Payload {
		if r.Name == name {
			return r.UID, nil
		}
	}

	return name, errors.Errorf("Could not find ID for role: %s", name)
}

// GetServiceAccount will return the ID for a service account name
func (c *GrafanaClient) GetServiceAccount(name string) (string, error) {
	var page int64
	for {
		params := service_accounts.NewSearchOrgServiceAccountsWithPagingParams().WithPage(&page).WithQuery(&name)
		resp, err := c.Client.ServiceAccounts.SearchOrgServiceAccountsWithPaging(params)
		if err != nil {
			return name, err
		}
		serviceAccounts := resp.Payload.ServiceAccounts
		if len(serviceAccounts) == 0 {
			break
		}
		for _, sa := range serviceAccounts {
			if sa.Name == name {
				return fmt.Sprintf("%d", sa.ID), nil
			}
		}
	}
	return name, errors.Errorf("Could not find ID for service account: %s", name)
}

// GetUser will return the ID for user login or email
func (c *GrafanaClient) GetUser(name string) (string, error) {
	var resp interface{ GetPayload() []*models.OrgUserDTO }

	params := org.NewGetOrgUsersForCurrentOrgParams().WithQuery(&name)
	resp, err := c.Client.Org.GetOrgUsersForCurrentOrg(params)
	if err != nil {
		return "", err
	}

	if len(resp.GetPayload()) == 0 {
		return "", errors.Errorf("could not find ID for user: %s", name)
	}

	for _, user := range resp.GetPayload() {
		if user.Email == name || user.Login == name {
			return fmt.Sprintf("%d", user.UserID), nil
		}
	}

	return name, errors.Errorf("Could not find ID for user: %s", name)
}
