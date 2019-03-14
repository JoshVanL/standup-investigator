package main

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/nlopes/slack"
	log "github.com/sirupsen/logrus"
)

func init() {
	log.SetOutput(os.Stderr)
	log.SetLevel(log.DebugLevel)
}

func timeFromStringFloat(timeString string) (time.Time, error) {
	timeFloat, err := strconv.ParseFloat(timeString, 64)
	if err != nil {
		return time.Time{}, err
	}
	sec, dec := math.Modf(timeFloat)
	return time.Unix(int64(sec), int64(dec*(1e9))), nil
}

type UserName string
type UserID string

func main() {
	api := slack.New(os.Getenv("SLACK_TOKEN"))

	groups, err := api.GetGroups(true)
	if err != nil {
		log.Fatalf("error getting private channels: %s", err)
	}

	// find correct channel
	var standUpChannel *slack.Group
	for _, group := range groups {
		if group.NameNormalized == "stand-ups" {
			standUpChannel = &group
			break
		}
	}

	if standUpChannel == nil {
		log.Fatal("error finding stand-ups private channel")
	}

	// get users
	usersByID := make(map[UserID]UserName)
	users, err := api.GetUsers()
	if err != nil {
		log.Fatalf("error getting users: %s", err)
	}
	for _, user := range users {
		if user.Deleted {
			continue
		}
		usersByID[UserID(user.ID)] = UserName(user.Name)
	}
	log.Debugf("found %d users in the channel", len(users))

	// retrieve last 1000 messages from stand-ups
	messages, err := api.GetGroupHistory(
		standUpChannel.ID,
		slack.HistoryParameters{
			Count:     1000,
			Oldest:    fmt.Sprintf("%d", time.Now().Add(-24*time.Hour).Unix()),
			Inclusive: true,
			Unreads:   false,
		},
	)
	if err != nil {
		log.Fatalf("error retrieving messages: %s", err)
	}

	log.Debugf("found %d messages in the last 24 hours", len(messages.Messages))
	hasJetbotApproval := func(m *slack.Message) bool {
		for _, reaction := range m.Reactions {
			if reaction.Name == "heavy_check_mark" {
				for _, userID := range reaction.Users {
					if userName, ok := usersByID[UserID(userID)]; ok && userName == "jetbot" {
						return true
					}
				}
			}
		}

		return false
	}

	// find last valid standup per user
	lastStandupByUserID := make(map[UserID]time.Time)
	validStandupCount := 0
	for _, message := range messages.Messages {
		if hasJetbotApproval(&message) {
			timestamp, err := timeFromStringFloat(message.Timestamp)
			if err != nil {
				continue
				log.Errorf("error parsing timestamp: %s", err)
			}
			lastStandupByUserID[UserID(message.Msg.User)] = timestamp
			validStandupCount += 1
		}
	}
	log.Debugf("found %d valid standups in the last 24 hours", validStandupCount)

	// post time of last stand-up
	now := time.Now()
	deadline := time.Date(now.Year(), now.Month(), now.Day(), 11, 01, 0, 0, now.Location())
	for _, user := range standUpChannel.Members {
		userID := UserID(user)
		userName, ok := usersByID[userID]
		// skip users not found
		if !ok {
			continue
		}

		// skip old bates and jetbot
		if userName == "jetbot" || userName == "mattbates" {
			continue
		}

		log := log.WithField("user", userName)
		if timestamp, ok := lastStandupByUserID[userID]; !ok {
			log.Error("no stand-up")
		} else {
			diff := deadline.Sub(timestamp)
			if diff > 0 {
				log.Infof("stand-up %s early", diff.String())
			} else {
				log.Warnf("stand-up %s late", (-1 * diff).String())
			}
		}
	}
}
