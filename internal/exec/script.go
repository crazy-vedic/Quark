package exec

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/crazy-vedic/quark/internal/domain"
)

// scriptTimeout bounds one script run. A script that loops forever is
// interrupted and reported as an error.
const scriptTimeout = 5 * time.Second

// ScriptLog collects console.log output from the scripts of one execution.
// Attach it to the context with WithScriptLog; hooks append to it when it is
// present. It is safe for concurrent use.
type ScriptLog struct {
	mu    sync.Mutex
	lines []string
}

// NewScriptLog returns an empty log.
func NewScriptLog() *ScriptLog { return &ScriptLog{} }

// Lines returns a copy of the logged lines in order.
func (l *ScriptLog) Lines() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

func (l *ScriptLog) add(line string) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, line)
}

type scriptLogKey struct{}

// WithScriptLog returns a context that carries log for script console output.
func WithScriptLog(ctx context.Context, log *ScriptLog) context.Context {
	return context.WithValue(ctx, scriptLogKey{}, log)
}

func scriptLogFrom(ctx context.Context) *ScriptLog {
	log, _ := ctx.Value(scriptLogKey{}).(*ScriptLog)
	return log
}

// ScriptCheck is the outcome of one assert() call in a post-response script,
// or of a script that threw. Rule names the check, Reason explains a failure.
type ScriptCheck struct {
	Rule   string
	Passed bool
	Reason string
}

// ScriptHook runs a request's pre-request script (before it is sent) and
// post-response script (after the response arrives). Scripts run in an
// embedded JavaScript engine with no file, network or module access.
//
// Pre-request scripts see req (method, url, body, headers) and env. Changes to
// req are applied to the request that is sent; an exception aborts it.
//
// Post-response scripts see res (status, time, body, headers, json()), env and
// assert(condition, message). Each assert adds a pass or fail result to
// ExecuteResult.Assertions. An exception adds a failed result.
//
// env.get and env.set use the request collection's active environment. Writes
// are saved when the script finishes without an exception.
type ScriptHook struct {
	env     *ExtractionHook // provides the environment lookup and writes
	timeout time.Duration
}

// NewScriptHook returns a hook that reads and writes environments through st.
func NewScriptHook(st ExtractionEnvStore) *ScriptHook {
	return &ScriptHook{env: NewExtractionHook(st), timeout: scriptTimeout}
}

var (
	_ PreRequestHook   = (*ScriptHook)(nil)
	_ PostResponseHook = (*ScriptHook)(nil)
)

// BeforeRequest implements PreRequestHook.
func (h *ScriptHook) BeforeRequest(
	ctx context.Context,
	req *domain.Request,
) (*domain.Request, error) {
	if req == nil || strings.TrimSpace(req.PreScript) == "" {
		return req, nil
	}
	headers, err := domain.ParseHeaders(req.Headers)
	if err != nil {
		return nil, fmt.Errorf("pre-request script: request headers: %w", err)
	}
	before := flattenHeaders(headers)

	vm := goja.New()
	scope := newScriptEnv(ctx, h.env, req.CollectionID)
	reqObj := vm.NewObject()
	must(reqObj.Set("method", req.Method))
	must(reqObj.Set("url", req.URL))
	must(reqObj.Set("body", req.Body))
	hdrObj := vm.NewObject()
	for k, v := range before {
		must(hdrObj.Set(k, v))
	}
	must(reqObj.Set("headers", hdrObj))
	must(vm.Set("req", reqObj))
	installConsole(vm, scriptLogFrom(ctx))
	installEnv(vm, scope)

	if err := h.run(vm, req.PreScript); err != nil {
		return nil, fmt.Errorf("pre-request script: %w", err)
	}
	if err := scope.flush(); err != nil {
		return nil, fmt.Errorf("pre-request script: %w", err)
	}

	out := *req
	out.Method = reqObj.Get("method").String()
	out.URL = reqObj.Get("url").String()
	out.Body = reqObj.Get("body").String()
	if after := readFlatHeaders(hdrObj); !sameFlatHeaders(before, after) {
		encoded, err := json.Marshal(after)
		if err != nil {
			return nil, fmt.Errorf("pre-request script: encode headers: %w", err)
		}
		out.Headers = string(encoded)
	}
	return &out, nil
}

// AfterResponse implements PostResponseHook.
func (h *ScriptHook) AfterResponse(
	ctx context.Context,
	req *domain.Request,
	res *ExecuteResult,
) error {
	if req == nil || res == nil || strings.TrimSpace(req.PostScript) == "" {
		return nil
	}
	body, err := ResultBody(res)
	if err != nil {
		res.ScriptChecks = append(res.ScriptChecks, ScriptCheck{
			Rule: "post-response script", Reason: err.Error(),
		})
		return nil
	}

	vm := goja.New()
	scope := newScriptEnv(ctx, h.env, req.CollectionID)
	resObj := vm.NewObject()
	must(resObj.Set("status", res.StatusCode))
	must(resObj.Set("time", float64(res.Duration.Milliseconds())))
	must(resObj.Set("body", string(body)))
	hdrObj := vm.NewObject()
	for k, vs := range res.Headers {
		must(hdrObj.Set(strings.ToLower(k), strings.Join(vs, ", ")))
	}
	must(resObj.Set("headers", hdrObj))
	must(resObj.Set("json", func(goja.FunctionCall) goja.Value {
		var v any
		if err := json.Unmarshal(body, &v); err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(v)
	}))
	must(vm.Set("res", resObj))

	var asserts []ScriptCheck
	must(vm.Set("assert", func(call goja.FunctionCall) goja.Value {
		ok := call.Argument(0).ToBoolean()
		msg := "assertion"
		if a := call.Argument(1); !goja.IsUndefined(a) {
			msg = a.String()
		}
		r := ScriptCheck{Rule: "assert: " + msg, Passed: ok}
		if !ok {
			r.Reason = "condition was false"
		}
		asserts = append(asserts, r)
		return goja.Undefined()
	}))
	installConsole(vm, scriptLogFrom(ctx))
	installEnv(vm, scope)

	runErr := h.run(vm, req.PostScript)
	if runErr == nil {
		runErr = scope.flush()
	}
	res.ScriptChecks = append(res.ScriptChecks, asserts...)
	if runErr != nil {
		res.ScriptChecks = append(res.ScriptChecks, ScriptCheck{
			Rule: "post-response script", Reason: runErr.Error(),
		})
	}
	return nil
}

// run executes src in vm, interrupting it after the hook's timeout.
func (h *ScriptHook) run(vm *goja.Runtime, src string) error {
	timer := time.AfterFunc(h.timeout, func() {
		vm.Interrupt(fmt.Sprintf("script timed out after %v", h.timeout))
	})
	defer timer.Stop()
	_, err := vm.RunString(src)
	return err
}

// scriptEnv gives a script read access to the active environment and buffers
// its writes until the script finishes.
type scriptEnv struct {
	ctx        context.Context
	hook       *ExtractionHook
	collection string
	loaded     bool
	vars       map[string]string
	writes     map[string]string
}

func newScriptEnv(ctx context.Context, hook *ExtractionHook, collectionID string) *scriptEnv {
	return &scriptEnv{
		ctx:        ctx,
		hook:       hook,
		collection: collectionID,
		writes:     map[string]string{},
	}
}

func (s *scriptEnv) get(name string) (string, bool) {
	if v, ok := s.writes[name]; ok {
		return v, true
	}
	if !s.loaded {
		s.loaded = true
		s.vars = map[string]string{}
		if env, err := s.hook.targetEnvironment(s.ctx, s.collection); err == nil {
			if vars, err := env.DecodeVars(); err == nil {
				s.vars = vars
			}
		}
	}
	v, ok := s.vars[name]
	return v, ok
}

// flush saves the buffered writes to the target environment.
func (s *scriptEnv) flush() error {
	if len(s.writes) == 0 {
		return nil
	}
	env, err := s.hook.targetEnvironment(s.ctx, s.collection)
	if err != nil {
		return fmt.Errorf("env.set: %w", err)
	}
	if err := writeVars(s.ctx, s.hook.store, env, s.writes); err != nil {
		return fmt.Errorf("env.set: %w", err)
	}
	s.writes = map[string]string{}
	return nil
}

func installEnv(vm *goja.Runtime, s *scriptEnv) {
	env := vm.NewObject()
	must(env.Set("get", func(call goja.FunctionCall) goja.Value {
		v, ok := s.get(call.Argument(0).String())
		if !ok {
			return goja.Undefined()
		}
		return vm.ToValue(v)
	}))
	must(env.Set("set", func(call goja.FunctionCall) goja.Value {
		s.writes[call.Argument(0).String()] = call.Argument(1).String()
		return goja.Undefined()
	}))
	must(vm.Set("env", env))
}

func installConsole(vm *goja.Runtime, log *ScriptLog) {
	console := vm.NewObject()
	must(console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		log.add(strings.Join(parts, " "))
		return goja.Undefined()
	}))
	must(vm.Set("console", console))
}

// must panics on errors from goja's object setup. They cannot occur for the
// plain values set here, so a panic means a programming error.
func must(err error) {
	if err != nil {
		panic(err)
	}
}

// flattenHeaders joins each header's values with ", " for the script view.
func flattenHeaders(h map[string][]string) map[string]string {
	out := make(map[string]string, len(h))
	for k, vs := range h {
		out[k] = strings.Join(vs, ", ")
	}
	return out
}

// readFlatHeaders reads the header object back after a script ran. Deleted
// keys are absent.
func readFlatHeaders(obj *goja.Object) map[string]string {
	out := map[string]string{}
	for _, k := range obj.Keys() {
		out[k] = obj.Get(k).String()
	}
	return out
}

func sameFlatHeaders(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
