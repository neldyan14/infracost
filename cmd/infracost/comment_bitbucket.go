package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/infracost/infracost/internal/apiclient"
	"github.com/infracost/infracost/internal/comment"
	"github.com/infracost/infracost/internal/config"
	"github.com/infracost/infracost/internal/output"
	"github.com/infracost/infracost/internal/ui"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var validCommentBitbucketBehaviors = []string{"update", "new", "delete-and-new"}

func commentBitbucketCmd(ctx *config.RunContext) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bitbucket",
		Short: "Post an Infracost comment to Bitbucket",
		Long:  "Post an Infracost comment to Bitbucket",
		Example: `  Update comment on a pull request:

      infracost comment bitbucket --repo my-org/my-repo --pull-request 3 --path infracost.json --bitbucket-token $BITBUCKET_TOKEN

  Post a new comment to a commit:

      infracost comment bitbucket --repo my-org/my-repo --commit 2ca7182 --path infracost.json --behavior delete-and-new --bitbucket-token $BITBUCKET_TOKEN`,
		ValidArgs: []string{"--", "-"},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx.SetContextValue("platform", "bitbucket")

			var err error

			serverURL, _ := cmd.Flags().GetString("bitbucket-server-url")
			token, _ := cmd.Flags().GetString("bitbucket-token")
			tag, _ := cmd.Flags().GetString("tag")
			extra := comment.BitbucketExtra{
				ServerURL: serverURL,
				Token:     token,
				Tag:       tag,
			}

			commit, _ := cmd.Flags().GetString("commit")
			prNumber, _ := cmd.Flags().GetInt("pull-request")
			repo, _ := cmd.Flags().GetString("repo")

			var commentHandler *comment.CommentHandler
			if prNumber != 0 {
				ctx.SetContextValue("targetType", "pull-request")

				commentHandler, err = comment.NewBitbucketPRHandler(ctx.Context(), repo, strconv.Itoa(prNumber), extra)
				if err != nil {
					return err
				}
			} else if commit != "" {
				ctx.SetContextValue("targetType", "commit")

				commentHandler, err = comment.NewBitbucketCommitHandler(ctx.Context(), repo, commit, extra)
				if err != nil {
					return err
				}
			} else {
				ui.PrintUsage(cmd)
				return fmt.Errorf("either --commit or --pull-request is required")
			}

			behavior, _ := cmd.Flags().GetString("behavior")
			if behavior != "" && !contains(validCommentBitbucketBehaviors, behavior) {
				ui.PrintUsage(cmd)
				return fmt.Errorf("--behavior only supports %s", strings.Join(validCommentBitbucketBehaviors, ", "))
			}
			ctx.SetContextValue("behavior", behavior)

			paths, _ := cmd.Flags().GetStringArray("path")

			body, err := buildCommentBody(cmd, ctx, paths, output.MarkdownOptions{
				WillUpdate:          prNumber != 0 && behavior == "update",
				WillReplace:         prNumber != 0 && behavior == "delete-and-new",
				IncludeFeedbackLink: true,
				BasicSyntax:         true,
			})
			if err != nil {
				return err
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if !dryRun {
				err = commentHandler.CommentWithBehavior(ctx.Context(), behavior, string(body))
				if err != nil {
					return err
				}

				pricingClient := apiclient.NewPricingAPIClient(ctx)
				err = pricingClient.AddEvent("infracost-comment", ctx.EventEnv())
				if err != nil {
					log.Errorf("Error reporting event: %s", err)
				}

				cmd.Println("Comment posted to Bitbucket")
			} else {
				cmd.Println(string(body))
				cmd.Println("Comment not posted to Bitbucket (--dry-run was specified)")
			}

			return nil
		},
	}

	cmd.Flags().String("behavior", "update", `Behavior when posting comment, one of:
  update (default)  Update latest comment
  new               Create a new comment
  delete-and-new    Delete previous matching comments and create a new comment`)
	_ = cmd.RegisterFlagCompletionFunc("behavior", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return validCommentBitbucketBehaviors, cobra.ShellCompDirectiveDefault
	})
	cmd.Flags().String("bitbucket-token", "", "Bitbucket access token. Use 'username:app-password' for Bitbucket Cloud and HTTP access token for Bitbucket Server")
	_ = cmd.MarkFlagRequired("bitbucket-token")
	cmd.Flags().String("commit", "", "Commit SHA to post comment on, mutually exclusive with pull-request")
	cmd.Flags().StringArrayP("path", "p", []string{}, "Path to Infracost JSON files, glob patterns need quotes")
	_ = cmd.MarkFlagRequired("path")
	_ = cmd.MarkFlagFilename("path", "json")
	cmd.Flags().Int("pull-request", 0, "Pull request number to post comment on")
	cmd.Flags().String("repo", "", "Repository in format workspace/repo")
	_ = cmd.MarkFlagRequired("repo")
	cmd.Flags().String("tag", "", "Customize special text used to detect comments posted by Infracost (placed at the bottom of a comment)")
	cmd.Flags().Bool("dry-run", false, "Generate comment without actually posting to Bitbucket")

	return cmd
}