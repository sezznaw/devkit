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
	{"method-title", "a gateway method's title (the first sentence of its comment, the /docs sidebar) is at most 12 characters and has no punctuation; details go after it"},
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
	{"idl-deps", "idl.deps exists and was written for the commit idl.lock names (make gen writes both; the deps gate at start waits for the generations in it)"},
	{"domain-file", "a gateway domain is one thrift file, one namespace and one handler package of the same lowercase name"},
	{"domain-path", "a domain's routes are /v1/<domain>/..."},
	{"domain-include", "a domain file includes only common.thrift"},
	{"handler-split", "an RPC handler.go holds no business methods and a handler file holds at most handler_methods (8) RPC methods"},
	{"callers-coverage", "every method of an RPC service's IDL is named in the callers rules of each conf profile (allow: [<the service>] when only it calls the method): with callers.default deny an unlisted method answers 1007"},
	{"rpc-uid-from-ctx", "an RPC method a player gateway may call (lint.player_gateways, via the conf callers rules) never acts on the uid in its request body: kitexx.LoginUID(ctx) / kitexx.CheckMemberUID(ctx, uid); `// @target-uid` on the IDL method when the uid is the object acted on (project rule)"},
	{"redis-pubsub", "no go-redis Subscribe / PSubscribe / Publish: cross-service notification is a Kafka event (events.<domain>), Pub/Sub is not durable, replayable or authorized per service"},
	{"event-owner", "Publish / PublishTx only to events.<own domain> (callbacks.* only from lint.vendor_service); another producer needs lint.event_producers"},
	{"redis-key-owner", "no string literal with another service's Redis key prefix and no import of its key-builder package (registry lint.redis_keys, e.g. idl/redis-keys.txt): read its RPC or subscribe to its events (project rule)"},
	{"model-owner", "no reference to a lib table model another service owns (lint.model_owners) and no rt.AutoMigrate of it; transitional reads go in lint.model_owner_exceptions (project rule)"},
	{"anonymous-coverage", "an RPC service's conf identity.anonymous lists exactly the methods tagged // @anonymous in its IDL (the contract a gateway may call without a login)"},
	{"public-anonymous", "a gateway's @public handlers and its middlewares only call RPC methods tagged // @anonymous in the callee's IDL; a call that carries a fresh login is marked //devkit:lint-ignore public-anonymous"},
	{"vendor-only", "third-party HTTP calls and providers: only in lint.vendor_service (project rule)"},
	{"report-only", "report.enabled only in lint.report_service (project rule)"},
	{"error-code", "business codes come from the errors_dir Thrift files, never a bare number (project rule)"},
	{"error-dup", "a code is declared once across errors_dir (project rule)"},
	{"error-range", "a file declares only codes in its error_ranges (project rule)"},
	{"error-comment", "every code constant has a comment line above it (project rule)"},
}
