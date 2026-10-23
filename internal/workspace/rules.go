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
	// total, credit, debit, money, stake, payout, bonus, turnover, commission)
	// for rules money-type and no-float-money.
	MoneyWords []string `yaml:"money_words"`
	// ErrorsFile is the business-code table (a Markdown table), relative to
	// the IDL directory (rule error-code). Superseded by ErrorsDir.
	ErrorsFile string `yaml:"errors_file"`
	// ErrorsDir is the directory of error-code Thrift files, relative to the
	// IDL directory: one file per domain, `const i32 Name = code` with a
	// comment line above (rules error-code, error-dup, error-range,
	// error-comment). Empty: the rules are off.
	ErrorsDir string `yaml:"errors_dir"`
	// ErrorRanges maps an errors file (without .thrift) to the code ranges
	// it may declare, "lo-hi" joined by commas (rule error-range).
	ErrorRanges map[string]string `yaml:"error_ranges"`
	// HandlerMethods is how many RPC methods one file under handler/ may
	// hold before rule handler-split asks for a split by topic (default 8).
	HandlerMethods int `yaml:"handler_methods"`
	// PlayerGateways 是玩家网关的服务名（本项目 [ser-api]）：按 conf 的 callers 规则它们调得到的 RPC
	// 方法，不能按请求体的 uid 办事，要从 ctx 取登录者（规则 rpc-uid-from-ctx）。空：规则不跑。
	PlayerGateways []string `yaml:"player_gateways"`
	// RedisKeys 是 Redis 业务键登记表，相对 IDL 目录（如 redis-keys.txt）：`<前缀> <所有者> [允许 ...]` 与
	// `pkg <lib 包后缀> <所有者> [允许 ...]`；别的服务的前缀和键构造包本服务不许碰（规则 redis-key-owner）。空：不跑。
	RedisKeys string `yaml:"redis_keys"`
	// EventProducers 是主题 → 额外允许发它的服务（规则 event-owner 默认只许发 events.<本服务领域>），给过渡期代发用。
	EventProducers map[string][]string `yaml:"event_producers"`
	// ModelOwners 是服务 → 它拥有的 lib/model/mysql 表结构体名（它 AutoMigrate）；别的服务引用或迁移它们会被
	// 规则 model-owner 拦。ModelOwnerExceptions 是服务 → 阶段 1 前暂时允许引用的别家模型。
	ModelOwners          map[string][]string `yaml:"model_owners"`
	ModelOwnerExceptions map[string][]string `yaml:"model_owner_exceptions"`
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

// SharedProject is the `project:` block of the IDL repository's devkit.yaml:
// the settings every developer's project directory shares (devkit pull
// copies them into <project>/devkit.yaml), as opposed to the lint rules.
type SharedProject struct {
	ModulePrefix string            `yaml:"module_prefix"`
	GoPrivate    bool              `yaml:"go_private"`
	Vars         map[string]string `yaml:"vars"`
	// TakeoverSSH is the ssh target `devkit takeover` connects to. [takeover]
	TakeoverSSH string `yaml:"takeover_ssh"`
}

// LoadSharedProject reads the `project:` block of <idlDir>/devkit.yaml; nil
// when the file or the block is missing.
func LoadSharedProject(idlDir string) (*SharedProject, error) {
	b, err := os.ReadFile(filepath.Join(idlDir, ConfigFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Project *SharedProject `yaml:"project"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Join(idlDir, ConfigFile), err)
	}
	return doc.Project, nil
}
