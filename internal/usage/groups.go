package usage

import (
	"context"
	"net/http"
	"time"

	"github.com/fikrilal/agy-auth/internal/agy"
)

type GroupBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description,omitempty"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	RemainingPercent  int     `json:"remaining_percent"`
	ResetTime         string  `json:"reset_time,omitempty"`
}

func (b GroupBucket) ResetAt() (time.Time, bool) {
	return parseReset(b.ResetTime)
}

type Group struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Buckets     []GroupBucket `json:"buckets"`
}

type Groups struct {
	Description string    `json:"description,omitempty"`
	Groups      []Group   `json:"groups"`
	UpdatedAt   time.Time `json:"updated_at"`
	Stale       bool      `json:"stale,omitempty"`
}

type GroupsProvider interface {
	FetchGroups(ctx context.Context, token *agy.TokenFile) (Groups, error)
}

func (c *Client) FetchGroups(ctx context.Context, token *agy.TokenFile) (Groups, error) {
	accessToken, err := c.accessToken(ctx, token)
	if err != nil {
		return Groups{}, err
	}
	project, _, err := c.loadCodeAssist(ctx, accessToken)
	if err != nil {
		return Groups{}, err
	}
	var summary quotaSummaryResponse
	status, err := c.post(ctx, "retrieveUserQuotaSummary", quotaSummaryRequest{Project: project}, accessToken, &summary)
	if err != nil {
		return Groups{}, err
	}
	if status < http.StatusOK || status > 299 {
		return Groups{}, statusError(status)
	}

	return groupsFromSummary(summary, c.now().UTC()), nil
}

func groupsFromSummary(summary quotaSummaryResponse, now time.Time) Groups {
	groups := Groups{
		Description: summary.Description,
		Groups:      []Group{},
		UpdatedAt:   now,
	}
	for _, source := range summary.Groups {
		group := Group{Name: source.DisplayName, Description: source.Description, Buckets: []GroupBucket{}}
		for _, bucket := range source.Buckets {
			if bucket.RemainingFraction == nil {
				continue
			}
			group.Buckets = append(group.Buckets, GroupBucket{
				ID:                bucket.BucketID,
				Name:              bucket.DisplayName,
				Description:       bucket.Description,
				Window:            bucket.Window,
				RemainingFraction: *bucket.RemainingFraction,
				RemainingPercent:  percent(*bucket.RemainingFraction),
				ResetTime:         bucket.ResetTime,
			})
		}
		groups.Groups = append(groups.Groups, group)
	}
	return groups
}
