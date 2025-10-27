package main

import (
	"encoding/json"
	"slices"

	onCallAPI "github.com/grafana/amixr-api-go-client"
	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	v1 "k8s.io/api/core/v1"

	"github.com/crossplane/function-sdk-go/errors"
)

// OnCallClients is a map of OnCallClient structs
type OnCallClients map[string]*OnCallClient

// OnCallClient is a client with convenience methods
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

	client, err := NewClientFromProviderConfig(providerConfig, credentials, "oncall")
	if err != nil {
		return nil, err
	}

	c := client.(*onCallAPI.Client)
	if c.Teams == nil {
		return nil, errors.Errorf("client is nil")
	}
	return &OnCallClient{
		Client: c,
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
		response, _, err := c.Client.Users.ListUsers(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to list oncall users")
		}

		allUsers = append(allUsers, response.Users...)

		if response.Next == nil {
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
		response, _, err := c.Client.Teams.ListTeams(options)
		if err != nil {
			return errors.Wrapf(err, "Failed to list oncall users")
		}

		allTeams = append(allTeams, response.Teams...)

		if response.Next == nil {
			break
		}
		page++
	}
	c.Teams = allTeams
	return nil
}

// GetUsers looks up users and returns the IDs
func (c *OnCallClient) GetUsers(userIDs []string) []string {
	newVal := []string{}
	for _, id := range userIDs {
		userID := c.GetUserID(id)
		newVal = append(newVal, userID)
	}
	return newVal
}

// GetRollingUsers looks up rolling users (a nested array)
func (c *OnCallClient) GetRollingUsers(val [][]string) [][]string {
	newVal := [][]string{}
	for _, userIDs := range val {
		usernames := c.GetUsers(userIDs)
		newVal = append(newVal, usernames)
	}
	return newVal
}

// GetUserID looks up a user
func (c *OnCallClient) GetUserID(id string) string {
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
	usernameEmailIDx := slices.IndexFunc(c.Users, func(c *onCallAPI.User) bool {
		return c.Username == id || c.Email == id
	})

	if usernameEmailIDx != -1 {
		return c.Users[usernameEmailIDx].ID
	}

	// ID, username or email not found, return as is
	// TODO: consider failing here
	return id
}

// GetTeamID looks up a team
func (c *OnCallClient) GetTeamID(id string) string {
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
	teamEmailIDx := slices.IndexFunc(c.Teams, func(c *onCallAPI.Team) bool {
		return c.Name == id || c.Email == id
	})

	if teamEmailIDx != -1 {
		return c.Teams[teamEmailIDx].ID
	}

	// ID, name or email not found, return as is
	// TODO: consider failing here
	return id
}

// GetScheduleID looks up a schedule
func (c *OnCallClient) GetScheduleID(id string) string {
	options := &onCallAPI.ListScheduleOptions{
		Name: id,
	}
	response, _, err := c.Client.Schedules.ListSchedules(options)
	if err != nil {
		return id
		// TODO: figure out how to handle errors
		// return errors.Wrapf(err, "Failed to list oncall schedules")
	}

	return response.Schedules[0].ID
}
