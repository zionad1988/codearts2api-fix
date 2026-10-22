package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codearts2api/internal/auth"
	"codearts2api/internal/pool"
	"codearts2api/internal/upstream"
)

// 上游对 max_tokens 有硬上限：超出时 InferHub 在 SSE 流里塞
// InferHub.001001005.400 错误帧，客户端会当成空响应无限重试。
// 云端下发的 max_tokens 是上下文口径（131072+），必须在转发前钳制。
func TestChatClampsMaxTokensBeforeUpstream(t *testing.T) {
	bodies := make(chan map[string]any, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body map[string]any
		raw, _ := io.ReadAll(r.Body)
		_ = body
		bodies <- map[string]any{"raw": string(raw)}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer srv.Close()

	a := auth.New("clamp-account", "tester", "domain", "token", "ak", "sk", time.Now().Add(time.Hour).Format(time.RFC3339), "", "")
	p, err := pool.New([]*auth.Auth{a}, pool.Config{MaxConcurrent: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	p.Accounts()[0].Client = upstream.NewWithChatEndpoint(5*time.Second, srv.URL)
	// 目录已发现：避免聊天挑号时触发惰性发现打真实上游（离线测试）。
	upstream.SetAccountModels("clamp-account", []upstream.ModelInfo{{ID: "GLM-5.2"}})
	h := NewHandler(Config{Pool: p, DefaultModel: "glm-5.2", MaxRotate: 1})

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"glm-5.2","max_tokens":131072,"messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := <-bodies
	raw, _ := got["raw"].(string)
	if !strings.Contains(raw, `"max_tokens":65536`) {
		t.Fatalf("max_tokens 应钳到 %d，上游收到：%s", upstream.MaxOutputTokens, raw)
	}
	if strings.Contains(raw, "131072") {
		t.Fatalf("不得把上下文口径的 max_tokens 透传给上游：%s", raw)
	}
}

// /v1/models 上报的输出上限也必须是上游真正接受的值，否则客户端照抄必挂。
func TestModelEntriesClampMaxOutputTokens(t *testing.T) {
	entries := modelEntries([]upstream.ModelInfo{
		{ID: "deepseek-v4-flash-0731", ContextWindow: 1048576, MaxTokens: 393216, Benefit: true},
		{ID: "GLM-5.2", ContextWindow: 202752, MaxTokens: 8192},
	})
	byID := map[string]map[string]any{}
	for _, e := range entries {
		byID[e["id"].(string)] = e
	}
	if got := byID["deepseek-v4-flash-0731"]["max_output_tokens"]; got != int64(upstream.MaxOutputTokens) {
		t.Errorf("超限的 max_output_tokens 应钳到 %d，got=%v", upstream.MaxOutputTokens, got)
	}
	// 未超限的保持不变：钳制只收上限，不该动正常值。
	if got := byID["GLM-5.2"]["max_output_tokens"]; got != int64(8192) {
		t.Errorf("未超限的 max_output_tokens 不应被改动，got=%v", got)
	}
}

// 混合账号池：聊天必须按「账号目录里有没有这个模型」挑号。
//
// 回归 8cf84bf 的合并事故：pickAccount 还在，但聊天主路径被换回普通轮转，
// 于是请求先发给没有该模型的账号，白吃 400（MaxRotate 默认只有 3 次），
// 健康账号还会被记错误、累计到阈值冷却 10 分钟。
func TestChatRoutesToAccountWithModel(t *testing.T) {
	var hitNoModel, hitHasModel int
	newChatServer := func(counter *int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*counter++
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
		}))
	}
	noModelSrv := newChatServer(&hitNoModel)
	defer noModelSrv.Close()
	hasModelSrv := newChatServer(&hitHasModel)
	defer hasModelSrv.Close()

	a := auth.New("acct-a", "a", "d", "token", "ak", "sk", time.Now().Add(time.Hour).Format(time.RFC3339), "", "")
	b := auth.New("acct-b", "b", "d", "token", "ak", "sk", time.Now().Add(time.Hour).Format(time.RFC3339), "", "")
	p, err := pool.New([]*auth.Auth{a, b}, pool.Config{MaxConcurrent: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	p.Accounts()[0].Client = upstream.NewWithChatEndpoint(5*time.Second, noModelSrv.URL)
	p.Accounts()[1].Client = upstream.NewWithChatEndpoint(5*time.Second, hasModelSrv.URL)

	upstream.SetAccountModels("acct-a", []upstream.ModelInfo{{ID: "GLM-5.2"}})
	upstream.SetAccountModels("acct-b", []upstream.ModelInfo{{ID: "GLM-5.2"}, {ID: "deepseek-v4-pro-0813", Benefit: true}})

	h := NewHandler(Config{Pool: p, DefaultModel: "glm-5.2", MaxRotate: 1})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"deepseek-v4-pro-0813","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if hitNoModel != 0 {
		t.Errorf("没有该模型的账号不该收到请求，hits=%d", hitNoModel)
	}
	if hitHasModel != 1 {
		t.Errorf("请求应发给目录里有该模型的账号，hits=%d", hitHasModel)
	}
}

// 粘性会话锁定的账号若目录里没有当前模型（中途换模型/套餐变更），必须换号。
func TestStickyAccountSwitchesWhenModelUnavailable(t *testing.T) {
	var hitSticky int
	stickySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitSticky++
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer stickySrv.Close()
	otherSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
	}))
	defer otherSrv.Close()

	a := auth.New("acct-a", "a", "d", "token", "ak", "sk", time.Now().Add(time.Hour).Format(time.RFC3339), "", "")
	b := auth.New("acct-b", "b", "d", "token", "ak", "sk", time.Now().Add(time.Hour).Format(time.RFC3339), "", "")
	p, err := pool.New([]*auth.Auth{a, b}, pool.Config{MaxConcurrent: 1}, "")
	if err != nil {
		t.Fatal(err)
	}
	p.Accounts()[0].Client = upstream.NewWithChatEndpoint(5*time.Second, stickySrv.URL)
	p.Accounts()[1].Client = upstream.NewWithChatEndpoint(5*time.Second, otherSrv.URL)
	upstream.SetAccountModels("acct-a", []upstream.ModelInfo{{ID: "GLM-5.2"}})
	upstream.SetAccountModels("acct-b", []upstream.ModelInfo{{ID: "GLM-5.2"}, {ID: "glm-5.3-flash", Benefit: true}})

	h := NewHandler(Config{Pool: p, DefaultModel: "glm-5.2", MaxRotate: 1})
	chatID := strings.Repeat("a", 32)
	h.convAcct[chatID] = "acct-a" // 上一轮会话锁在 acct-a

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(
		`{"model":"glm-5.3-flash","conversation_id":"`+chatID+`","messages":[{"role":"user","content":"hi"}]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if hitSticky != 0 {
		t.Errorf("粘性账号没有该模型时会注定 400，不该再发给它，hits=%d", hitSticky)
	}
}

// 强制同步带节流：一次查不到后的窗口内不得再打上游（客户端可能配错模型名并重试）。
func TestForceSyncModelsThrottled(t *testing.T) {
	p, err := pool.New([]*auth.Auth{{UserID: "ts", UserName: "ts"}}, pool.Config{}, "")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{Pool: p, Upstream: upstream.New(time.Second)})
	// 停用账号：只测节流窗口，不让测试打真实上游（离线用例）。
	p.Disable("ts", "test")

	forcedSync.Lock()
	forcedSync.last = time.Time{}
	forcedSync.Unlock()

	h.forceSyncModels("definitely-not-a-model")
	forcedSync.Lock()
	afterFirst := forcedSync.last
	forcedSync.Unlock()
	if afterFirst.IsZero() {
		t.Fatal("首次查不到应记下同步时间")
	}

	start := time.Now()
	h.forceSyncModels("definitely-not-a-model")
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("节流窗口内应立即返回，got=%s", elapsed)
	}
	forcedSync.Lock()
	unchanged := forcedSync.last.Equal(afterFirst)
	forcedSync.Unlock()
	if !unchanged {
		t.Fatal("节流窗口内不应再发起同步")
	}
}