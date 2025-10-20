package main

import (
	"encoding/json"
	"slices"

	"github.com/crossplane/function-sdk-go/errors"

	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	v1 "k8s.io/api/core/v1"

	onCallAPI "github.com/grafana/amixr-api-go-client"
)

type OnCallClients map[string]*OnCallClient

type OnCallClient struct {
	Client *onCallAPI.Client
	Users  []*onCallAPI.User
	Teams  []*onCallAPI.Team
}

// NewOnCallClient returns a client with convenience methods
func NewOnCallClient(providerConfig *v1beta1.ProviderConfig, secret *v1.Secret) (*OnCallClient, error) {
	var credentials map[string]string
	err := json.Unmarshal(secret.Data["instanceCredentials"], &credentials)
	if err != nil {
		return nil, err
	}

	if providerConfig.Spec.OnCallURL != "" {
		credentials["oncall_url"] = providerConfig.Spec.OnCallURL
	}

	if providerConfig.Spec.URL != "" {
		credentials["url"] = providerConfig.Spec.URL
	}
	client, err := onCallAPI.NewWithGrafanaURL(credentials["oncall_url"], credentials["auth"], credentials["url"])
	if err != nil {
		return nil, err
	}
	return &OnCallClient{
		Client: client,
	}, nil
}

func (c *OnCallClient) getAllUsers() error {
	allUsers := []*onCallAPI.User{}
	page := 1
	for {
		options := &onCallAPI.ListUserOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		usersResponse, _, err := c.Client.Users.ListUsers(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to list oncall users")
		}

		allUsers = append(allUsers, usersResponse.Users...)

		if usersResponse.PaginatedResponse.Next == nil {
			break
		}
		page++
	}
	c.Users = allUsers
	return nil
}

func (c *OnCallClient) getAllTeams() error {
	allTeams := []*onCallAPI.Team{}
	page := 1
	for {
		options := &onCallAPI.ListTeamOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		teamsResponse, _, err := c.Client.Teams.ListTeams(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to list oncall users")
		}

		allTeams = append(allTeams, teamsResponse.Teams...)

		if teamsResponse.PaginatedResponse.Next == nil {
			break
		}
		page++
	}
	c.Teams = allTeams
	return nil
}

// GetUsers looks up users and returns the IDs
func (c *OnCallClient) GetUsers(userIDs []string) []string {
	var newVal []string
	for _, id := range userIDs {
		userID := c.GetUserId(id)
		newVal = append(newVal, userID)
	}
	return newVal
}

// GetRollingUsers looks up rolling users (a nested array)
func (c *OnCallClient) GetRollingUsers(val [][]string) [][]string {
	var newVal [][]string
	for _, userIDs := range val {
		usernames := c.GetUsers(userIDs)
		newVal = append(newVal, usernames)
	}
	return newVal
}

// GetUserId looks up a user
func (c *OnCallClient) GetUserId(id string) string {
	// populate the list if the list is empty
	if len(c.Users) == 0 {
		err := c.getAllUsers()
		if err != nil {
			return id
		}
	}

	// check if the provided ID exists
	idx := slices.IndexFunc(c.Users, func(c *onCallAPI.User) bool {
		return c.ID == id
	})
	if idx != -1 {
		return c.Users[idx].ID
	}

	// if the provided ID does not exist, try to look up by username or email
	usernameEmailIdx := slices.IndexFunc(c.Users, func(c *onCallAPI.User) bool {
		return c.Username == id || c.Email == id
	})

	if usernameEmailIdx != -1 {
		return c.Users[usernameEmailIdx].ID
	}

	// ID, username or email not found, return as is
	// TODO: consider failing here
	return id
}

// GetTeamId looks up a team
func (c *OnCallClient) GetTeamId(id string) string {
	if len(c.Teams) == 0 {
		err := c.getAllTeams()
		if err != nil {
			return id
		}
	}

	// check if the provided ID exists
	idx := slices.IndexFunc(c.Teams, func(c *onCallAPI.Team) bool {
		return c.ID == id
	})
	if idx != -1 {
		return c.Users[idx].ID
	}

	// if the provided ID does not exist, try to look up by username or email
	teamEmailIdx := slices.IndexFunc(c.Teams, func(c *onCallAPI.Team) bool {
		return c.Name == id || c.Email == id
	})

	if teamEmailIdx != -1 {
		return c.Teams[teamEmailIdx].ID
	}

	// ID, name or email not found, return as is
	// TODO: consider failing here
	return id
}
