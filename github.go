package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/google/go-github/v92/github"
	"github.com/patrickmn/go-cache"
)

func getJobs(c *cache.Cache, owner, repo string) Dashboard {
	data, found := c.Get(fmt.Sprintf("%s-%s", owner, repo))
	if found {
		log.Println("cache found")
		return data.(Dashboard)
	}

	log.Println("cache not found")

	client, err := github.NewClient(
		github.WithAuthToken(os.Getenv("GITHUB_TOKEN")),
		github.WithTimeout(30*time.Second),
		github.WithEnvProxy(),
	)
	if err != nil {
		log.Fatalf("create GitHub client: %v", err)
	}

	windowStart := time.Now().Add(time.Duration(-12) * time.Hour).UTC().Format(time.RFC3339)
	opt := &github.ListWorkflowRunsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
		Created:     ">=" + windowStart,
	}

	var runs []*github.WorkflowRun
	for {
		resp, rr, err := client.Actions.ListRepositoryWorkflowRuns(context.Background(), owner, repo, opt)
		if rlErr, ok := err.(*github.RateLimitError); ok { //nolint: errorlint
			slog.Warn("GitHub API rate limited", "reset", rlErr.Rate.Reset.Time)
			time.Sleep(time.Until(rlErr.Rate.Reset.Time))
			continue
		} else if err != nil {
			slog.Error("list workflow runs", "owner", owner, "repo", repo, "error", err)
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

			opts := &github.ListOptions{PerPage: 100}
			pull, _, err := client.PullRequests.ListPullRequestsWithCommit(context.Background(), owner, repo, run.GetHeadSHA(), opts)
			if rlErr, ok := err.(*github.RateLimitError); ok { //nolint: errorlint
				slog.Warn("GitHub API rate limited", "reset", rlErr.Rate.Reset.Time)
				time.Sleep(time.Until(rlErr.Rate.Reset.Time))
				continue
			} else if err != nil {
				slog.Error("list pull requests", "owner", owner, "repo", repo, "error", err)
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
