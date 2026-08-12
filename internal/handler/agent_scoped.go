package handler

import "net/http"

func (d *Dependencies) AgentGetSettings(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.GetSettings(w, r)
}

func (d *Dependencies) AgentUpdateSettings(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.UpdateSettings(w, r)
}

func (d *Dependencies) AgentGetPersonalization(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.GetPersonalization(w, r)
}

func (d *Dependencies) AgentUpdatePersonalization(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.UpdatePersonalization(w, r)
}

func (d *Dependencies) AgentListProviders(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.ListProviders(w, r)
}

func (d *Dependencies) AgentConnectProviderKey(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.ConnectProviderKey(w, r)
}

func (d *Dependencies) AgentStartProviderOAuth(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.StartProviderOAuth(w, r)
}

func (d *Dependencies) AgentDisconnectProvider(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.DisconnectProvider(w, r)
}

func (d *Dependencies) AgentGetProviderOAuthAttempt(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.GetProviderOAuthAttempt(w, r)
}

func (d *Dependencies) AgentCompleteProviderOAuth(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.CompleteProviderOAuth(w, r)
}

func (d *Dependencies) AgentCancelProviderOAuth(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.CancelProviderOAuth(w, r)
}

func (d *Dependencies) AgentProxyGlobalSSE(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.ProxyGlobalSSE(w, r)
}

func (d *Dependencies) AgentProxyJSON(w http.ResponseWriter, r *http.Request) {
	d.AgentDeps.ProxyJSON(w, r)
}
