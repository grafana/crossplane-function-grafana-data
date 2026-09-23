package main

import (
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	onCallAPI "github.com/grafana/amixr-api-go-client"
)

func TestGetTeamID(t *testing.T) {

	const teamOneName = "team-one"

	type args struct {
		client *OnCallClient
		id     string
	}
	type want struct {
		id  string
		err error
	}

	cases := map[string]struct {
		reason string
		args   args
		want   want
	}{
		"TeamMatchedByIDWithNoUsersLoaded": {
			reason: "GetTeamID should return the team ID when the team list holds a matching ID and the user list is empty",
			args: args{
				client: &OnCallClient{
					Teams: []*onCallAPI.Team{
						{ID: "T1", Name: teamOneName, Email: "team-one@example.com"},
					},
				},
				id: "T1",
			},
			want: want{id: "T1"},
		},
		"TeamMatchedByIDWithUsersLoaded": {
			reason: "GetTeamID should return the team ID, not the ID of the user at the same index",
			args: args{
				client: &OnCallClient{
					Teams: []*onCallAPI.Team{
						{ID: "T1", Name: teamOneName},
						{ID: "T2", Name: "team-two"},
					},
					Users: []*onCallAPI.User{
						{ID: "U1", Username: "user-one"},
						{ID: "U2", Username: "user-two"},
					},
				},
				id: "T2",
			},
			want: want{id: "T2"},
		},
		"TeamMatchedByName": {
			reason: "GetTeamID should fall back to a name match and return that team's ID",
			args: args{
				client: &OnCallClient{
					Teams: []*onCallAPI.Team{
						{ID: "T1", Name: teamOneName},
					},
				},
				id: teamOneName,
			},
			want: want{id: "T1"},
		},
		"TeamMatchedByEmail": {
			reason: "GetTeamID should fall back to an email match and return that team's ID",
			args: args{
				client: &OnCallClient{
					Teams: []*onCallAPI.Team{
						{ID: "T1", Name: teamOneName, Email: "team-one@example.com"},
					},
				},
				id: "team-one@example.com",
			},
			want: want{id: "T1"},
		},
		"TeamNotFound": {
			reason: "GetTeamID should return an error when no team matches",
			args: args{
				client: &OnCallClient{
					Teams: []*onCallAPI.Team{
						{ID: "T1", Name: teamOneName},
					},
				},
				id: "team-three",
			},
			want: want{err: cmpopts.AnyError},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			id, err := tc.args.client.GetTeamID(tc.args.id)

			if diff := cmp.Diff(tc.want.id, id); diff != "" {
				t.Errorf("%s\nGetTeamID(...): -want id, +got id:\n%s", tc.reason, diff)
			}

			if diff := cmp.Diff(tc.want.err, err, cmpopts.EquateErrors()); diff != "" {
				t.Errorf("%s\nGetTeamID(...): -want err, +got err:\n%s", tc.reason, diff)
			}
		})
	}
}
