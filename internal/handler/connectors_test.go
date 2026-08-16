package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"marvo/internal/control"
	"marvo/internal/store"
)

func TestConnectorManagementRedactsCredentialsAndIsolatesUsers(t *testing.T) {
	var delivered atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		delivered.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	fixture := newMultiuserFixture(t)
	userA := fixture.createUser(t, "Connector A")
	userB := fixture.createUser(t, "Connector B")
	sessionA := loginUserAdminForTest(t, fixture, userA)

	providers := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userA.User.ID+"/admin/connectors/providers", nil, sessionA)
	if providers.Code != http.StatusOK {
		t.Fatalf("providers status = %d, body = %s", providers.Code, providers.Body.String())
	}
	var catalog struct {
		Providers []any `json:"providers"`
	}
	if err := json.Unmarshal(providers.Body.Bytes(), &catalog); err != nil || len(catalog.Providers) != 101 {
		t.Fatalf("provider catalog = %d, %v", len(catalog.Providers), err)
	}

	credentialURL := target.URL + "/hooks/private-token"
	created := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/admin/connectors", map[string]any{
		"provider_id": "webhook", "name": "自动化", "enabled": true,
		"config": map[string]any{"url": credentialURL, "method": "POST", "content_type": "json"},
	}, sessionA)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", created.Code, created.Body.String())
	}
	if bytes.Contains(created.Body.Bytes(), []byte(credentialURL)) || !bytes.Contains(created.Body.Bytes(), []byte(`"url":true`)) {
		t.Fatalf("create response exposed or failed to mark credential: %s", created.Body.String())
	}
	var item connectorResponse
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatal(err)
	}

	updated := serveJSON(t, fixture.mux, http.MethodPut, "/api/user/"+userA.User.ID+"/admin/connectors/"+item.ID, map[string]any{
		"name": "自动化更新", "enabled": true,
		"config": map[string]any{"method": "POST", "content_type": "json"},
	}, sessionA)
	if updated.Code != http.StatusOK || bytes.Contains(updated.Body.Bytes(), []byte(credentialURL)) {
		t.Fatalf("update status = %d, body = %s", updated.Code, updated.Body.String())
	}
	spaceA := fixture.space(t, userA.User.ID)
	stored, err := spaceA.Connectors.Get(item.ID)
	if err != nil || stored.Config["url"] != credentialURL {
		t.Fatalf("preserved connector = %#v, %v", stored, err)
	}
	tested := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+userA.User.ID+"/admin/connectors/test", map[string]any{
		"connector_id": item.ID,
		"config":       map[string]any{},
	}, sessionA)
	if tested.Code != http.StatusOK || delivered.Load() != 1 {
		t.Fatalf("saved connector test status = %d, delivered = %d, body = %s", tested.Code, delivered.Load(), tested.Body.String())
	}
	if _, _, err := spaceA.Activity.Publish(store.ActivityPublish{
		Kind: store.ActivityKindNotice, Title: "完成", Content: "结果", SourceSessionID: "s", SourceMessageID: "m",
	}); err != nil {
		t.Fatal(err)
	}
	if summary, err := spaceA.Connectors.Summary(item.ID); err != nil || summary.Pending != 1 {
		t.Fatalf("delivery summary = %#v, %v", summary, err)
	}

	crossed := serveJSON(t, fixture.mux, http.MethodGet, "/api/user/"+userB.User.ID+"/admin/connectors", nil, sessionA)
	if crossed.Code != http.StatusUnauthorized {
		t.Fatalf("A admin accessed B connectors with status %d", crossed.Code)
	}
}

func loginUserAdminForTest(t *testing.T, fixture *multiuserFixture, user *control.Enrollment) *http.Cookie {
	t.Helper()
	response := serveJSON(t, fixture.mux, http.MethodPost, "/api/user/"+user.User.ID+"/auth/verify", map[string]any{
		"password": "a sufficiently long password",
	})
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == userAdminCookieName(user.User.ID) {
			return cookie
		}
	}
	t.Fatal("user admin session cookie was not set")
	return nil
}
