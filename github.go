package main

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/patrickmn/go-cache"
)

const cacheDuration = 15 * time.Minute

func getJobs(ctx context.Context, c *cache.Cache, client *github.Client, owner, repo string) (Dashboard, error) {
	if err := ctx.Err(); err != nil {
		return Dashboard{}, err
	}
	key := owner + "/" + repo
	if data, found := c.Get(key); found {
		return data.(Dashboard), nil
	}
	opt := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Created:     ">=" + time.Now().Add(-12*time.Hour).UTC().Format(time.RFC3339),
	}
	var runs []*github.WorkflowRun
	for {
		resp, page, err := client.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opt)
		if err != nil {
			return Dashboard{}, fmt.Errorf("list workflow runs for %s/%s: %w", owner, repo, err)
		}
		runs = append(runs, resp.WorkflowRuns...)
		if page.NextPage == 0 {
			break
		}
		opt.Page = page.NextPage
	}
	report := make(map[string][]Status)
	for _, run := range runs {
		conclusion := run.GetConclusion()
		switch run.GetStatus() {
		case "in_progress":
			conclusion = "progress"
		case "queued":
			conclusion = "queued"
		}
		prInfo := ""
		if run.GetEvent() == "pull_request" || run.GetEvent() == "pull_request_target" {
			opts := &github.ListOptions{PerPage: 100}
			var pulls []*github.PullRequest
			for {
				pull, page, err := client.PullRequests.ListPullRequestsWithCommit(ctx, owner, repo, run.GetHeadSHA(), opts)
				if err != nil {
					return Dashboard{}, fmt.Errorf("list pull requests for %s/%s: %w", owner, repo, err)
				}
				pulls = append(pulls, pull...)
				if page.NextPage == 0 {
					break
				}
				opts.Page = page.NextPage
			}
			if len(pulls) == 1 {
				prInfo = pulls[0].GetHTMLURL()
			}
		}
		report[run.GetName()] = append(report[run.GetName()], Status{
			SHA:         run.GetHeadSHA(),
			Conclusion:  conclusion,
			TableStatus: getTableStatus(conclusion),
			Status:      run.GetStatus(),
			JobHTML:     run.GetHTMLURL(),
			WorkflowID:  run.GetWorkflowID(),
			CreatedAt:   run.GetCreatedAt().Time,
			Event:       run.GetEvent(),
			PRUrl:       prInfo,
		})
	}
	now := time.Now()
	dash := Dashboard{
		Owner:          owner,
		Repo:           repo,
		DateGenerated:  now.Local().Format(time.RFC3339),
		NextGeneration: now.Add(cacheDuration).Local().Format(time.RFC3339),
		Data:           report,
	}
	c.Set(key, dash, cacheDuration)
	return dash, nil
}

func getTableStatus(conclusion string) string {
	switch conclusion {
	case "success":
		return "success"
	case "failure":
		return "danger"
	case "queued":
		return "info"
	case "cancelled":
		return "secondary"
	default:
		return "warning"
	}
}
