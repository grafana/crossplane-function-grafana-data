package main

import (
	"slices"

	onCallAPI "github.com/grafana/amixr-api-go-client"

	"github.com/crossplane/function-sdk-go/errors"
	"github.com/crossplane/function-sdk-go/resource"
)

// OnCallClient is a client with convenience methods
type OnCallClient struct {
	Client *onCallAPI.Client
	Users  []*onCallAPI.User
	Teams  []*onCallAPI.Team
}

// NewOnCallClient returns a client with convenience methods
func NewOnCallClient(client *onCallAPI.Client) *OnCallClient {
	return &OnCallClient{
		Client: client,
	}
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
		path := "spec.forProvider.users"
		if err := replacePath(desired, path, c.GetUsers); err != nil {
			return err
		}

		path = "spec.forProvider.rollingUsers"
		return replacePath(desired, path, c.GetRollingUsers)

	case "Schedule":
		path := "spec.forProvider.teamId"
		return replacePath(desired, path, c.GetTeamID)

	case "UserNotificationRule":
		path := "spec.forProvider.userId"
		return replacePath(desired, path, c.GetUsers)

	case "Integration":
		path := "spec.forProvider.defaultRoute.slack.channelId"
		return replacePath(desired, path, c.GetSlackChannelID)
	}

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
	// populate the list if the list is empty
	if len(c.Users) == 0 {
		err := c.getAllUsers()
		if err != nil {
			return "", err
		}
	}

	// check if the provided ID exists
	idx := slices.IndexFunc(c.Users, func(c *onCallAPI.User) bool {
		return c.ID == id
	})
	if idx != -1 {
		return c.Users[idx].ID, nil
	}

	// if the provided ID does not exist, try to look up by username or email
	usernameEmailIDx := slices.IndexFunc(c.Users, func(c *onCallAPI.User) bool {
		return c.Username == id || c.Email == id
	})

	if usernameEmailIDx != -1 {
		return c.Users[usernameEmailIDx].ID, nil
	}

	return "", errors.Errorf("Could not find user with name %s", id)
}

// GetTeamID looks up a team
func (c *OnCallClient) GetTeamID(id string) (string, error) {
	if len(c.Teams) == 0 {
		err := c.getAllTeams()
		if err != nil {
			return "", err
		}
	}

	// check if the provided ID exists
	idx := slices.IndexFunc(c.Teams, func(c *onCallAPI.Team) bool {
		return c.ID == id
	})
	if idx != -1 {
		return c.Users[idx].ID, nil
	}

	// if the provided ID does not exist, try to look up by username or email
	teamEmailIDx := slices.IndexFunc(c.Teams, func(c *onCallAPI.Team) bool {
		return c.Name == id || c.Email == id
	})

	if teamEmailIDx != -1 {
		return c.Teams[teamEmailIDx].ID, nil
	}

	return "", errors.Errorf("Could not find team with ID %s", id)
}

// GetScheduleID looks up a schedule
func (c *OnCallClient) GetScheduleID(id string) (string, error) {
	options := &onCallAPI.ListScheduleOptions{
		Name: id,
	}
	response, _, err := c.Client.Schedules.ListSchedules(options)
	if err != nil {
		return "", errors.Wrapf(err, "Failed to list oncall schedules")
	}

	return response.Schedules[0].ID, nil
}

func (c *OnCallClient) GetSlackChannelID(name string) (string, error) {
	options := &onCallAPI.ListSlackChannelOptions{
		ChannelName: name,
	}

	slackChannelsResponse, _, err := c.Client.SlackChannels.ListSlackChannels(options)
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
