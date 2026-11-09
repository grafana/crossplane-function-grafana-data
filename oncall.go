package main

import (
	"slices"

	onCallAPI "github.com/grafana/amixr-api-go-client"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/resource"
)

const (
	pathTeamID = "spec.forProvider.teamId"
)

// OnCallClient is a client with convenience methods
type OnCallClient struct {
	Client *onCallAPI.Client
	Users  []*onCallAPI.User
	Teams  []*onCallAPI.Team

	// cacheKey scopes the shared user and team caches to one OnCall instance
	// and its credentials. Empty disables the caches.
	cacheKey string
}

// NewOnCallClient returns a client with convenience methods. providerConfigName
// scopes the shared user and team caches, so different OnCall instances or
// tokens never share a list.
func NewOnCallClient(client *onCallAPI.Client, providerConfigName string) *OnCallClient {
	key := providerConfigName
	if client != nil {
		if u := client.BaseURL(); u != nil {
			key += "|" + u.String()
		}
	}
	return &OnCallClient{
		Client:   client,
		cacheKey: key,
	}
}

// ensureUsers fills c.Users if it is empty, from the shared cache when enabled.
func (c *OnCallClient) ensureUsers() error {
	if len(c.Users) > 0 {
		return nil
	}
	var users []*onCallAPI.User
	var err error
	if c.cacheKey == "" {
		users, err = c.getAllUsers()
	} else {
		users, err = onCallUsersCache.get(c.cacheKey, c.getAllUsers)
	}
	if err != nil {
		return err
	}
	c.Users = users
	return nil
}

// refreshUsers reloads c.Users after a lookup miss. It reports whether a new
// list was loaded; the shared cache limits how often that happens.
func (c *OnCallClient) refreshUsers() (bool, error) {
	if c.cacheKey == "" {
		return false, nil
	}
	users, refreshed, err := onCallUsersCache.refresh(c.cacheKey, c.getAllUsers)
	if err != nil || !refreshed {
		return false, err
	}
	c.Users = users
	return true, nil
}

// ensureTeams fills c.Teams if it is empty, from the shared cache when enabled.
func (c *OnCallClient) ensureTeams() error {
	if len(c.Teams) > 0 {
		return nil
	}
	var teams []*onCallAPI.Team
	var err error
	if c.cacheKey == "" {
		teams, err = c.getAllTeams()
	} else {
		teams, err = onCallTeamsCache.get(c.cacheKey, c.getAllTeams)
	}
	if err != nil {
		return err
	}
	c.Teams = teams
	return nil
}

// refreshTeams reloads c.Teams after a lookup miss. It reports whether a new
// list was loaded; the shared cache limits how often that happens.
func (c *OnCallClient) refreshTeams() (bool, error) {
	if c.cacheKey == "" {
		return false, nil
	}
	teams, refreshed, err := onCallTeamsCache.refresh(c.cacheKey, c.getAllTeams)
	if err != nil || !refreshed {
		return false, err
	}
	c.Teams = teams
	return true, nil
}

func (c *OnCallClient) getAllUsers() ([]*onCallAPI.User, error) {
	allUsers := []*onCallAPI.User{}
	page := 1
	for {
		options := &onCallAPI.ListUserOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		response, _, err := c.Client.Users.ListUsers(options) //nolint:bodyclose // The OnCall client's Do method closes the response body.
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to list oncall users")
		}

		allUsers = append(allUsers, response.Users...)

		if response.Next == nil {
			break
		}
		page++
	}
	return allUsers, nil
}

// Process processes fields of different kinds
//
//nolint:gocyclo // switch statement complexity increases with resource types
func (c *OnCallClient) Process(desired *resource.DesiredComposed) error {
	gvk := desired.Resource.GroupVersionKind()
	switch gvk.Kind {
	case "Escalation":
		path := "spec.forProvider.notifyOnCallFromSchedule"
		if err := replacePath(desired, path, c.GetScheduleID); err != nil {
			return err
		}

		path = "spec.forProvider.personsToNotify"
		if err := replacePath(desired, path, c.GetUsers); err != nil {
			return err
		}

		path = "spec.forProvider.personsToNotifyNextEachTime"
		return replacePath(desired, path, c.GetUsers)

	case "OnCallShift":
		path := pathTeamID
		if err := replacePath(desired, path, c.GetTeamID); err != nil {
			return err
		}

		path = "spec.forProvider.users"
		if err := replacePath(desired, path, c.GetUsers); err != nil {
			return err
		}

		path = "spec.forProvider.rollingUsers"
		return replacePath(desired, path, c.GetRollingUsers)

	case "Schedule":
		path := pathTeamID
		return replacePath(desired, path, c.GetTeamID)

	case "UserNotificationRule":
		path := "spec.forProvider.userId"
		return replacePath(desired, path, c.GetUsers)

	case "Integration":
		path := pathTeamID
		if err := replacePath(desired, path, c.GetTeamID); err != nil {
			return err
		}

		path = "spec.forProvider.defaultRoute.slack.channelId"
		return replacePath(desired, path, c.GetSlackChannelID)

	case "EscalationChain":
		path := pathTeamID
		return replacePath(desired, path, c.GetTeamID)
	}

	return nil
}

func (c *OnCallClient) getAllTeams() ([]*onCallAPI.Team, error) {
	allTeams := []*onCallAPI.Team{}
	page := 1
	for {
		options := &onCallAPI.ListTeamOptions{
			ListOptions: onCallAPI.ListOptions{
				Page: page,
			},
		}
		response, _, err := c.Client.Teams.ListTeams(options) //nolint:bodyclose // The OnCall client's Do method closes the response body.
		if err != nil {
			return nil, errors.Wrapf(err, "Failed to list oncall teams")
		}

		allTeams = append(allTeams, response.Teams...)

		if response.Next == nil {
			break
		}
		page++
	}
	return allTeams, nil
}

// GetUsers looks up users and returns the IDs
func (c *OnCallClient) GetUsers(userIDs []string) ([]string, error) {
	newVal := []string{}
	for _, id := range userIDs {
		userID, err := c.GetUserID(id)
		if err != nil {
			return nil, err
		}
		newVal = append(newVal, userID)
	}
	return newVal, nil
}

// GetRollingUsers looks up rolling users (a nested array)
func (c *OnCallClient) GetRollingUsers(val [][]string) ([][]string, error) {
	newVal := [][]string{}
	for _, userIDs := range val {
		usernames, err := c.GetUsers(userIDs)
		if err != nil {
			return nil, err
		}
		newVal = append(newVal, usernames)
	}
	return newVal, nil
}

// GetUserID looks up a user
func (c *OnCallClient) GetUserID(id string) (string, error) {
	if err := c.ensureUsers(); err != nil {
		return "", err
	}
	if userID, ok := findUserID(c.Users, id); ok {
		return userID, nil
	}

	// The cached list may predate the user, so reload it once and retry.
	refreshed, err := c.refreshUsers()
	if err != nil {
		return "", err
	}
	if refreshed {
		if userID, ok := findUserID(c.Users, id); ok {
			return userID, nil
		}
	}

	return "", errors.Errorf("Could not find user with name %s", id)
}

func findUserID(users []*onCallAPI.User, id string) (string, bool) {
	// check if the provided ID exists
	idx := slices.IndexFunc(users, func(u *onCallAPI.User) bool {
		return u.ID == id
	})
	if idx != -1 {
		return users[idx].ID, true
	}

	// if the provided ID does not exist, try to look up by username or email
	idx = slices.IndexFunc(users, func(u *onCallAPI.User) bool {
		return u.Username == id || u.Email == id
	})
	if idx != -1 {
		return users[idx].ID, true
	}

	return "", false
}

// GetTeamID looks up a team
func (c *OnCallClient) GetTeamID(id string) (string, error) {
	if err := c.ensureTeams(); err != nil {
		return "", err
	}
	if teamID, ok := findTeamID(c.Teams, id); ok {
		return teamID, nil
	}

	// The cached list may predate the team, so reload it once and retry.
	refreshed, err := c.refreshTeams()
	if err != nil {
		return "", err
	}
	if refreshed {
		if teamID, ok := findTeamID(c.Teams, id); ok {
			return teamID, nil
		}
	}

	return "", errors.Errorf("Could not find team with ID %s", id)
}

func findTeamID(teams []*onCallAPI.Team, id string) (string, bool) {
	// check if the provided ID exists
	idx := slices.IndexFunc(teams, func(t *onCallAPI.Team) bool {
		return t.ID == id
	})
	if idx != -1 {
		return teams[idx].ID, true
	}

	// if the provided ID does not exist, try to look up by name or email
	idx = slices.IndexFunc(teams, func(t *onCallAPI.Team) bool {
		return t.Name == id || t.Email == id
	})
	if idx != -1 {
		return teams[idx].ID, true
	}

	return "", false
}

// GetScheduleID looks up a schedule
func (c *OnCallClient) GetScheduleID(id string) (string, error) {
	options := &onCallAPI.ListScheduleOptions{
		Name: id,
	}
	response, _, err := c.Client.Schedules.ListSchedules(options) //nolint:bodyclose // The OnCall client's Do method closes the response body.
	if err != nil {
		return "", errors.Wrapf(err, "Failed to list oncall schedules")
	}

	if len(response.Schedules) == 0 {
		return "", errors.Wrapf(err, "No schedules found for name %s", id)
	}

	return response.Schedules[0].ID, nil
}

// GetSlackChannelID looks up a slack channel ID
func (c *OnCallClient) GetSlackChannelID(name string) (string, error) {
	options := &onCallAPI.ListSlackChannelOptions{
		ChannelName: name,
	}

	slackChannelsResponse, _, err := c.Client.SlackChannels.ListSlackChannels(options) //nolint:bodyclose // The OnCall client's Do method closes the response body.
	if err != nil {
		return "", err
	}

	if len(slackChannelsResponse.SlackChannels) == 0 {
		return name, nil
	} else if len(slackChannelsResponse.SlackChannels) != 1 {
		return name, nil
	}

	slackChannel := slackChannelsResponse.SlackChannels[0]

	return slackChannel.SlackId, nil
}
