package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/google/go-github/v53/github"
	"github.com/patrickmn/go-cache"
	"golang.org/x/oauth2"
)

func getJobs(c *cache.Cache, owner, repo string) Dashboard {
	data, found := c.Get(fmt.Sprintf("%s-%s", owner, repo))
	if found {
		log.Println("cache found")
		return data.(Dashboard)
	}

	log.Println("cache not found")

	ctx := context.Background()
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)

	client := github.NewClient(tc)

	windowStart := time.Now().Add(time.Duration(-12) * time.Hour).UTC().Format(time.RFC3339)
	opt := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Created:     ">=" + windowStart,
	}

	var runs []*github.WorkflowRun
	for {
		resp, rr, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), owner, repo, opt)
		if rlErr, ok := err.(*github.RateLimitError); ok { //nolint: errorlint
			log.Printf("ListRepositoryWorkflowRuns ratelimited. Pausing until %s", rlErr.Rate.Reset.Time.String())
			time.Sleep(time.Until(rlErr.Rate.Reset.Time))
			continue
		} else if err != nil {
			log.Printf("ListRepositoryWorkflowRuns error for repo %s/%s: %s", owner, repo, err.Error())
			os.Exit(1)
		}

		runs = append(runs, resp.WorkflowRuns...)
		if rr.NextPage == 0 {
			break
		}
		opt.Page = rr.NextPage
	}

	var report = make(map[string][]Status)
	for _, run := range runs {
		conclusion := run.GetConclusion()
		if run.GetStatus() == "in_progress" {
			conclusion = "progress"
		} else if run.GetStatus() == "queued" {
			conclusion = "queued"
		}

		tableStatus := getTableStatus(conclusion)

		prInfo := ""
		if run.GetEvent() == "pull_request" || run.GetEvent() == "pull_request_target" {
			owner = run.GetRepository().GetOwner().GetLogin()
			repo = run.GetRepository().GetName()

			opts := &github.PullRequestListOptions{
				State: "all",
			}
			pull, _, err := client.PullRequests.ListPullRequestsWithCommit(context.Background(), owner, repo, run.GetHeadSHA(), opts)
			if rlErr, ok := err.(*github.RateLimitError); ok { //nolint: errorlint
				log.Printf("ListRepositoryWorkflowRuns ratelimited. Pausing until %s", rlErr.Rate.Reset.Time.String())
				time.Sleep(time.Until(rlErr.Rate.Reset.Time))
				continue
			} else if err != nil {
				log.Printf("ListPullRequestsWithCommit error for repo %s/%s: %s", owner, repo, err.Error())
				os.Exit(1)
			}

			prInfo = ""
			if len(pull) == 1 {
				prInfo = pull[0].GetHTMLURL()
			}
		}

		_, ok := report[run.GetName()]
		if ok {
			report[run.GetName()] = append(report[run.GetName()], Status{
				SHA:         run.GetHeadSHA(),
				Conclusion:  conclusion,
				TableStatus: tableStatus,
				Status:      run.GetStatus(),
				JobHTML:     run.GetHTMLURL(),
				WorkflowID:  run.GetWorkflowID(),
				CreatedAt:   run.GetCreatedAt().Time,
				Event:       run.GetEvent(),
				PRUrl:       prInfo,
			})
		} else {
			report[run.GetName()] = []Status{
				{
					SHA:         run.GetHeadSHA(),
					Conclusion:  conclusion,
					TableStatus: tableStatus,
					Status:      run.GetStatus(),
					JobHTML:     run.GetHTMLURL(),
					WorkflowID:  run.GetWorkflowID(),
					CreatedAt:   run.GetCreatedAt().Time,
					Event:       run.GetEvent(),
					PRUrl:       prInfo,
				},
			}
		}
	}

	dash := Dashboard{
		Owner:          owner,
		Repo:           repo,
		DateGenerated:  time.Now().Local().Format(time.RFC3339),
		NextGeneration: time.Now().Add(15 * time.Minute).Local().Format(time.RFC3339),
		Data:           report,
	}

	c.Set(fmt.Sprintf("%s-%s", owner, repo), dash, 15*time.Minute)
	return dash
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
