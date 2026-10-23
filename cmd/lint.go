package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/sezznaw/devkit/internal/lint"
	"github.com/sezznaw/devkit/internal/project"
	"github.com/sezznaw/devkit/internal/workspace"
)

var lintCmd = &cobra.Command{
	Use:   "lint",
	Short: "Check the service against the team's rules (the ones in idl/AGENTS.md)",
	Long: `lint checks a service for the team's conventions, whoever (or whatever) wrote
the code. Generic rules, the same in every project: no middleware opened by
hand (rt.DB, rt.Redis, rt.Kafka, rt.S3, rt.Provider instead), request_id on
write methods, a comment on every IDL method, framework files untouched, zlog
for logging, idl.lock current, files as keys (<name>_key in, <name>_url out;
one GetUploadURL per service, nothing uploaded through the gateway). Project
rules, from <idl>/devkit.yaml:
  lint:
    vendor_service: ser-vendor      # third-party calls only here (rule vendor-only; unset: off)
    report_service: ser-report      # the one service that may open the report database (report-only; unset: off)
    money_words: [wager]            # added to the built-in money words (amount, balance, stake, payout, fee, ...)
    errors_dir: errors              # error-code Thrift files, one per domain (unset: rule off)
    error_ranges: {member: 2000-2999} # the codes each file may declare
    handler_methods: 8              # RPC methods one handler file may hold (handler-split)
    player_gateways: [ser-api]      # RPC methods these may call take the login from ctx, not req.Uid (rpc-uid-from-ctx; unset: off)
    disable: []                     # rules this project does not want (names: devkit lint --rules)

Run it inside a service (make lint does), or in the project directory for
every service. A line can opt out of one rule with a comment:
  //devkit:lint-ignore <rule>        (Go)
  // devkit:lint-ignore <rule>       (Thrift)
Exit status 1 when there are findings, so CI stops.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if lintFlags.rules {
			for _, r := range lint.Rules {
				fmt.Printf("%-24s %s\n", r.Name, r.Summary)
			}
			return nil
		}
		var roots []string
		if root, err := project.FindRoot("."); err == nil {
			roots = []string{root}
		} else if dir := workspace.Find("."); dir != "" {
			if roots, err = project.Services(dir); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("run devkit lint inside a service or in the project directory")
		}
		projectDir := workspace.Find(roots[0])
		idlDir := lintFlags.idl
		if idlDir == "" && projectDir != "" {
			idlDir = filepath.Join(projectDir, "idl")
		}
		if idlDir != "" {
			if st, err := os.Stat(idlDir); err != nil || !st.IsDir() {
				fmt.Fprintf(os.Stderr, "lint: IDL directory %s not found; the IDL rules are skipped (pass --idl)\n", idlDir)
				idlDir = ""
			}
		}
		// Project rules live in the IDL repository (idl/devkit.yaml), so the
		// laptop and CI read the same file.
		var rules workspace.ProjectRules
		if idlDir != "" {
			r, err := workspace.LoadProjectRules(idlDir)
			if err != nil {
				return err
			}
			rules = r
		}
		total := 0
		for i, root := range roots {
			pd := ""
			if i == 0 {
				pd = projectDir // the kit-common/ checkout is checked once per run
			}
			findings, err := lint.Run(lint.Options{Root: root, IDLDir: idlDir, ProjectDir: pd,
				VendorService: rules.Lint.VendorService, ReportService: rules.Lint.ReportService, MoneyWords: rules.Lint.MoneyWords, ErrorsFile: rules.Lint.ErrorsFile, ErrorsDir: rules.Lint.ErrorsDir, ErrorRanges: rules.Lint.ErrorRanges, HandlerMethods: rules.Lint.HandlerMethods, PlayerGateways: rules.Lint.PlayerGateways, RedisKeysFile: rules.Lint.RedisKeys, EventProducers: rules.Lint.EventProducers, ModelOwners: rules.Lint.ModelOwners, ModelOwnerExceptions: rules.Lint.ModelOwnerExceptions, Disable: rules.Lint.Disable})
			if err != nil {
				return err
			}
			for _, f := range findings {
				rel := f.File
				if len(roots) > 1 {
					rel = filepath.Join(filepath.Base(root), f.File)
				}
				f.File = rel
				fmt.Fprintln(os.Stdout, f.String())
			}
			total += len(findings)
		}
		if total > 0 {
			fmt.Fprintf(os.Stderr, "\n%d finding(s). Each line says what to do instead; idl/AGENTS.md has the reasons.\n", total)
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "lint: no findings")
		return nil
	},
}

var lintFlags struct {
	idl   string
	rules bool
}

func init() {
	lintCmd.Flags().StringVar(&lintFlags.idl, "idl", "", "the IDL checkout (default: <project>/idl; CI passes $IDL_DIR)")
	lintCmd.Flags().BoolVar(&lintFlags.rules, "rules", false, "list every rule with one line each (the name goes after //devkit:lint-ignore or in devkit.yaml disable) and exit")
	rootCmd.AddCommand(lintCmd)
}
