package workspace

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// RulesFile is the project's shared devkit settings, kept in the IDL
// repository next to AGENTS.md and ports.yaml so that every developer's
// machine and every CI job read the same file. devkit.yaml in the project
// directory stays personal (module_prefix, idl_repo).
const RulesFile = "devkit.yaml"

// ProjectRules is <idl>/devkit.yaml.
type ProjectRules struct {
	Lint LintRules `yaml:"lint"`
}

// LintRules configures the project-specific lint rules; the generic rules
// (middleware, zlog, framework files, method comments, request_id, idl.lock)
// need no configuration.
type LintRules struct {
	// ReportService is the one service allowed to open the report database
	// (report.enabled, rule report-only). Empty: the rule is off.
	ReportService string `yaml:"report_service"`
	// VendorService is the one service allowed to call third parties
	// (rule vendor-only). Empty: the rule is off.
	VendorService string `yaml:"vendor_service"`
	// MoneyWords are added to the built-in list (amount, balance, price, fee,
	// total, credit, debit, money) for rule no-float-money.
	MoneyWords []string `yaml:"money_words"`
	// ErrorsFile is the business-code table, relative to the IDL directory
	// (rule error-code). Empty: the rule is off.
	ErrorsFile string `yaml:"errors_file"`
	// Disable lists rules this project does not want.
	Disable []string `yaml:"disable"`
}

// LoadProjectRules reads <idlDir>/devkit.yaml; a missing file is empty rules.
func LoadProjectRules(idlDir string) (ProjectRules, error) {
	var r ProjectRules
	data, err := os.ReadFile(filepath.Join(idlDir, RulesFile))
	if errors.Is(err, os.ErrNotExist) {
		return r, nil
	}
	if err != nil {
		return r, err
	}
	if err := yaml.Unmarshal(data, &r); err != nil {
		return r, fmt.Errorf("parse %s: %w", filepath.Join(idlDir, RulesFile), err)
	}
	return r, nil
}
