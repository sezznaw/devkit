package lint

// Rule is one lint rule as `devkit lint --rules` lists it: the name to put
// after //devkit:lint-ignore or in devkit.yaml's disable, and what it checks.
type Rule struct {
	Name    string
	Summary string
}

// Rules is every rule, generic ones first, then the ones a project turns on
// through idl/devkit.yaml. Keep it in step with the Finding{...} literals.
var Rules = []Rule{
	{"no-direct-middleware", "no gorm.Open / redis.NewClient / Kafka, HTTP, S3 clients / NewTicker by hand, and no Kitex server.WithLimit: use rt.DB, rt.Redis, rt.Kafka, rt.Provider, rt.S3, the limits: config"},
	{"use-zlog", "no fmt.Print* / log.* for logging: zlog"},
	{"schema-in-lib", "no DDL, db.AutoMigrate or TableName() in a service: table structs live in the project's lib"},
	{"error-msg", "a business error is errcode.Err(errcode.X), not a hand-written message"},
	{"envelope-write", "a gateway handler ends with hertzx.OK / hertzx.Fail, never c.JSON"},
	{"bind", "a gateway handler binds with hertzx.Bind, not c.BindAndValidate"},
	{"lock", "no SetNX by hand: redisx.WithLock"},
	{"mutex", "no sync.Mutex in a handler or repo: it only locks one replica"},
	{"request-id-source", "a request_id passed downstream comes from the request being handled (req.RequestId, kitexx.RequestID / SubRequestID), never a new UUID, a timestamp or a literal"},
	{"delay", "no time.AfterFunc / NewTimer / sleep in a goroutine: rt.Delay.Schedule"},
	{"outbox", "no Publish inside a transaction: PublishTx"},
	{"no-float-money", "money is never a float: common.Money in the IDL, moneyx.Money in Go"},
	{"money-type", "a field whose name says money (amount, balance, stake, ...) is common.Money"},
	{"rate-type", "a field whose name says ratio (odds, rate, percent, ...) is common.Decimal"},
	{"request-id", "a write method's request starts with request_id"},
	{"method-comment", "every IDL method has a comment (its title is the docs)"},
	{"paging", "list paging is common.PageReq / common.PageInfo, not hand-written page fields"},
	{"no-envelope", "no code / msg in a response struct: the gateway adds the envelope"},
	{"limit", "login-like methods (login, register, password, code) carry // @limit"},
	{"file-field", "files travel as <name>_key in requests and <name>_url in responses, never a URL, a path or the content itself"},
	{"upload-method", "one GetUploadURL(kind) per service, no per-field upload methods"},
	{"upload-through-gateway", "no FormFile / multipart / S3.Put in a gateway handler: the client uploads to a presigned link"},
	{"tests", "a handler package has at least one test"},
	{"framework-file", "framework files (Makefile, Dockerfile, CI, cmd/<svc>/main.go, ...) are not edited: devkit update owns them"},
	{"common-version", "go.mod's kit-common version equals the template's (devkit update changes it)"},
	{"common-checkout", "the project's kit-common/ checkout has no local changes"},
	{"idl-lock", "idl.lock exists, is not behind the IDL checkout, the IDL has no uncommitted changes and the locked commit is pushed"},
	{"domain-file", "a gateway domain is one thrift file, one namespace and one handler package of the same lowercase name"},
	{"domain-path", "a domain's routes are /v1/<domain>/..."},
	{"domain-include", "a domain file includes only common.thrift"},
	{"handler-split", "an RPC handler.go holds no business methods and a handler file holds at most handler_methods (8) RPC methods"},
	{"vendor-only", "third-party HTTP calls and providers: only in lint.vendor_service (project rule)"},
	{"report-only", "report.enabled only in lint.report_service (project rule)"},
	{"error-code", "business codes come from the errors_dir Thrift files, never a bare number (project rule)"},
	{"error-dup", "a code is declared once across errors_dir (project rule)"},
	{"error-range", "a file declares only codes in its error_ranges (project rule)"},
	{"error-comment", "every code constant has a comment line above it (project rule)"},
}
