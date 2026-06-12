# Go MCP SDK Reference — `github.com/modelcontextprotocol/go-sdk`

Verified 2026-06-12 against the official repo and pkg.go.dev. This is the SDK RuntimePulse's MCP server uses (server side, stdio transport). Exact type/function names matter — re-check with `go doc github.com/modelcontextprotocol/go-sdk/mcp <Name>` if anything drifts.

## Version & stability

* Pin **v1.6.1** (latest stable; `go list -m -versions` confirms v1.0.0 → v1.6.1 released). Module `github.com/modelcontextprotocol/go-sdk`, primary package `.../mcp`.
* Requires **Go 1.25+** (we run 1.26.4). Stable v1.x; only client-side OAuth is experimental (behind `-tags mcp_go_client_oauth`) — irrelevant to a stdio server.
* Dependencies are moderate, CGO-free: `google/jsonschema-go`, `segmentio/encoding`, `golang-jwt/jwt/v5`, `golang.org/x/{oauth2,time,tools}`, `yosida95/uritemplate`. Consistent with our no-CGO stack.

Source: https://github.com/modelcontextprotocol/go-sdk · https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp

## Minimal stdio server

```go
type Input struct {
	Name string `json:"name" jsonschema:"the name of the person to greet"`
}
type Output struct {
	Greeting string `json:"greeting" jsonschema:"the greeting"`
}

func SayHi(ctx context.Context, req *mcp.CallToolRequest, in Input) (*mcp.CallToolResult, Output, error) {
	return nil, Output{Greeting: "Hi " + in.Name}, nil
}

func main() {
	server := mcp.NewServer(&mcp.Implementation{Name: "greeter", Version: "v1.0.0"}, nil)
	mcp.AddTool(server, &mcp.Tool{Name: "greet", Description: "say hi"}, SayHi)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
```

Verified signatures:
* `func NewServer(impl *Implementation, opts *ServerOptions) *Server` — `opts` may be nil.
* `func AddTool[In, Out any](s *Server, t *Tool, h ToolHandlerFor[In, Out])` — **package-level generic** (the typed variant). The method `(*Server).AddTool` is the untyped variant — don't use it.
* `func (s *Server) Run(ctx, Transport) error` — blocks until the client disconnects.
* `&mcp.StdioTransport{}` — zero-value literal as the transport.

## Tool handler contract

```go
type ToolHandlerFor[In, Out any] func(context.Context, *CallToolRequest, In) (*CallToolResult, Out, error)
```

* The `In` value arrives **already unmarshaled and schema-validated** — never parse JSON yourself.
* Returning the `Out` value populates `result.StructuredOutput`; if `CallToolResult.Content` is unset it is auto-filled with the JSON of `Out`. Returning a **nil `*CallToolResult` is allowed** when you only care about `Out`/error.
* **Error semantics (load-bearing):** with the typed `AddTool`, a non-nil Go `error` becomes a **tool error** (`IsError: true`, packed into content) the model can read — NOT a protocol failure that kills the connection. (The untyped variant turns errors into protocol errors.) → Use typed `AddTool` for all tools so a daemon-dial failure surfaces to the agent instead of dropping the MCP session.

## Input schemas

Auto-derived from the `In` struct when `Tool.InputSchema` is nil. `In` must be a struct or map (object schema). The `jsonschema:"..."` tag supplies each property's **description**; Go type → JSON type. Required-vs-optional is decided by `google/jsonschema-go` v0.4.3 — **pointer / `omitempty` fields are treated as optional**; confirm exact rules with that package if a field's requiredness matters. `In = any` → empty object schema.

## Gotchas for our use

* **stdout is the protocol stream** — the server binary must never write logs/output to stdout. Route all logging to stderr or a file.
* **Handlers may be invoked concurrently** (dispatch model UNVERIFIED — assume parallel). Our daemon client (`internal/client`) dials a fresh unix socket per `Call`/`Follow`, so it is already goroutine-safe; keep it that way.
* **Honor the handler `ctx`** for any blocking I/O. `wait_for_event` especially must select on `ctx.Done()` so a cancelled MCP request unblocks the daemon-side wait.
* `server.Run` blocking for the process lifetime is correct for a long-lived stdio server spawned by Claude Code (`claude mcp add --transport stdio runtimepulse -- runtimepulse mcp`).

## In RuntimePulse

The MCP server is a thin client: each tool handler dials the daemon over the existing unix socket (`internal/client`) and calls an existing RPC. Tool → RPC mapping: `create_watch`→`watch.add`, `create_rule`→`rule.add` (self-registration via agent+repoPath), `cancel_rule`→`rule.remove`, `get_events`→`events.list`, `wait_for_event`→live `events.follow` stream filtered client-side with a timeout.
