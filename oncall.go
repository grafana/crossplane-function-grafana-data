package main

import (
	"fmt"
	//"slices"

	"github.com/crossplane/function-sdk-go/errors"

	"github.com/grafana/crossplane-provider-grafana/apis/v1beta1"
	v1 "k8s.io/api/core/v1"

	onCallAPI "github.com/grafana/amixr-api-go-client"
)

type OnCallClients map[string]*OnCallClient

type OnCallClient struct {
	Client *onCallAPI.Client
	Users  []OnCallUser
}

type OnCallUser struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Role     string `json:"role"`
}

func NewOnCallClient(providerConfig *v1beta1.ProviderConfig, secret *v1.Secret) *OnCallClient {

	return &OnCallClient{}
}

//func (f *Function) getCreds(input v1beta1.Input, req *fnv1.RunFunctionRequest, keyName, secretKeyName string) (string, string, error) {
//	// use the go function SDK to retrieve the raw credentials secret
//	creds, err := request.GetCredentials(req, "grafana-oncall-creds")
//	if err != nil {
//		return "", "", err
//	}
//	rawCreds := string(creds.Data["instanceCredentials"])
//
//	// unmarshal instanceCredentials
//
//	accessKeyID := cfg.Section("default").Key(keyName).String()
//	secretAccessKey := cfg.Section("default").Key(secretKeyName).String()
//	return accessKeyID, secretAccessKey, nil
//}

func (c *OnCallClient) getAllOnCallUsers() error {
	allUsers := []OnCallUser{}
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

		for _, user := range usersResponse.Users {
			allUsers = append(allUsers, OnCallUser{
				ID:       user.ID,
				Username: user.Username,
				Email:    user.Email,
				Role:     user.Role,
			})
		}

		if usersResponse.PaginatedResponse.Next == nil {
			break
		}
		page++
	}
	return nil
}

func (c *OnCallClient) GetUsers(userIds []string) []string {
	var newVal []string
	for _, id := range userIds {
		userId := c.GetUserId(id)
		newVal = append(newVal, userId)
	}
	return newVal
}

func (c *OnCallClient) GetRollingUsers(val [][]string) [][]string {
	var newVal [][]string
	for _, userIds := range val {
		usernames := c.GetUsers(userIds)
		newVal = append(newVal, usernames)
	}
	return newVal
}

func (c *OnCallClient) GetUserId(id string) string {
	return fmt.Sprintf("changed_%s", id)
	// populate the list if the list is empty
	//if len(c.Users) == 0 {
	//	c.getAllOnCallUsers()
	//}

	//// check if the provided ID exists
	//idx := slices.IndexFunc(c.Users, func(c OnCallUser) bool {
	//	return c.ID == id
	//})
	//if idx != -1 {
	//	return c.Users[idx].ID
	//}

	//// if the provided ID does not exist, try to look up by username or email
	//usernameEmailIdx := slices.IndexFunc(c.Users, func(c OnCallUser) bool {
	//	return c.Username == id || c.Email == id
	//})
	//if usernameEmailIdx != -1 {
	//	return c.Users[usernameEmailIdx].ID
	//}

	//// ID, username or email not found, return as is
	//// TODO: consider failing here
	//return id
}

func (c *OnCallClient) GetTeamId(name string) string {
	return fmt.Sprintf("changed_%s", name)
}
