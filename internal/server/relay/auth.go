package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	oauthStateCookieName = "bonsai_relay_oauth_state"
	oauthStateTTL        = 10 * time.Minute
	relaySessionTTL      = 4 * time.Hour
	maxGitHubResponse    = 2 << 20
)

type githubTokenResponse struct {
	AccessToken string `json:"access_token"`
	Error       string `json:"error"`
}

func randomSecret() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (s *Server) authLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	state, err := randomSecret()
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.store.PutOAuthState(SecretHash(state), time.Now().UTC().Add(oauthStateTTL)); err != nil {
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookieName, Value: state, Path: "/auth/github/callback",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		MaxAge: int(oauthStateTTL.Seconds()),
	})
	query := url.Values{
		"client_id": {s.githubClientID},
		"redirect_uri": {s.externalURL + "/auth/github/callback"},
		"state": {state},
	}
	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+query.Encode(), http.StatusFound)
}

func (s *Server) authCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	cookie, err := r.Cookie(oauthStateCookieName)
	if err != nil || state == "" || code == "" ||
		subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		http.Error(w, "invalid OAuth state", http.StatusBadRequest)
		return
	}
	valid, err := s.store.ConsumeOAuthState(SecretHash(state), time.Now().UTC())
	if err != nil {
		http.Error(w, "authentication unavailable", http.StatusServiceUnavailable)
		return
	}
	if !valid {
		http.Error(w, "expired OAuth state", http.StatusBadRequest)
		return
	}
	token, err := s.exchangeGitHubCode(r.Context(), code)
	if err != nil {
		http.Error(w, "GitHub sign-in failed", http.StatusUnauthorized)
		return
	}
	userID, grants, err := s.githubIdentityAndRepositories(r.Context(), token)
	if err != nil || userID <= 0 {
		http.Error(w, "GitHub authorization unavailable", http.StatusUnauthorized)
		return
	}
	secret, err := randomSecret()
	if err != nil {
		http.Error(w, "session unavailable", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	session := RelaySession{
		GitHubUserID: userID, Repositories: grants,
		CreatedAt: now, ExpiresAt: now.Add(relaySessionTTL),
	}
	if err := s.store.PutSession(SecretHash(secret), session); err != nil {
		http.Error(w, "session unavailable", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: relayCookieName, Value: secret, Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Expires: session.ExpiresAt,
	})
	http.SetCookie(w, &http.Cookie{
		Name: oauthStateCookieName, Path: "/auth/github/callback",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	http.Redirect(w, r, s.frontendOrigin+"/", http.StatusFound)
}

func (s *Server) authLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if r.Header.Get("Origin") != s.frontendOrigin {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	if cookie, err := r.Cookie(relayCookieName); err == nil && cookie.Value != "" {
		key := SecretHash(cookie.Value)
		if err := s.store.DeleteSession(key); err != nil {
			http.Error(w, "logout unavailable", http.StatusServiceUnavailable)
			return
		}
		s.hub.closeSession(key)
	}
	http.SetCookie(w, &http.Cookie{
		Name: relayCookieName, Path: "/",
		Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
	w.Header().Set("Access-Control-Allow-Origin", s.frontendOrigin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Add("Vary", "Origin")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) authSession(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.frontendOrigin {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	_, session, ok := s.authenticatedSession(r)
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Access-Control-Allow-Origin", s.frontendOrigin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Add("Vary", "Origin")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"github_user_id": session.GitHubUserID,
		"repositories": session.Repositories,
		"expires_at": session.ExpiresAt,
	})
}

func (s *Server) exchangeGitHubCode(ctx context.Context, code string) (string, error) {
	values := url.Values{
		"client_id": {s.githubClientID},
		"client_secret": {s.githubClientSecret},
		"code": {code},
		"redirect_uri": {s.externalURL + "/auth/github/callback"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", bytes.NewBufferString(values.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("GitHub OAuth returned %s", res.Status)
	}
	var out githubTokenResponse
	if err := json.NewDecoder(io.LimitReader(res.Body, maxGitHubResponse)).Decode(&out); err != nil {
		return "", err
	}
	if out.AccessToken == "" || out.Error != "" {
		return "", errors.New("GitHub OAuth did not return an access token")
	}
	return out.AccessToken, nil
}

func (s *Server) githubJSON(ctx context.Context, endpoint, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("GitHub API returned %s", res.Status)
	}
	return json.NewDecoder(io.LimitReader(res.Body, maxGitHubResponse)).Decode(out)
}

func (s *Server) githubIdentityAndRepositories(ctx context.Context, token string) (int64, []RepositoryGrant, error) {
	var user struct {
		ID int64 `json:"id"`
	}
	if err := s.githubJSON(ctx, "https://api.github.com/user", token, &user); err != nil {
		return 0, nil, err
	}
	if user.ID <= 0 {
		return 0, nil, errors.New("invalid GitHub user")
	}
	var installations struct {
		Installations []struct {
			ID int64 `json:"id"`
		} `json:"installations"`
	}
	if err := s.githubJSON(ctx, "https://api.github.com/user/installations?per_page=100", token, &installations); err != nil {
		return 0, nil, err
	}
	grants := make([]RepositoryGrant, 0)
	seen := map[string]bool{}
	for _, installation := range installations.Installations {
		if installation.ID <= 0 {
			continue
		}
		for page := 1; page <= 100; page++ {
			var response struct {
				Repositories []struct {
					ID       int64  `json:"id"`
					FullName string `json:"full_name"`
				} `json:"repositories"`
			}
			endpoint := "https://api.github.com/user/installations/" + strconv.FormatInt(installation.ID, 10) +
				"/repositories?per_page=100&page=" + strconv.Itoa(page)
			if err := s.githubJSON(ctx, endpoint, token, &response); err != nil {
				return 0, nil, err
			}
			for _, repo := range response.Repositories {
				if repo.ID <= 0 {
					continue
				}
				key := strconv.FormatInt(installation.ID, 10) + ":" + strconv.FormatInt(repo.ID, 10)
				if seen[key] {
					continue
				}
				seen[key] = true
				grants = append(grants, RepositoryGrant{
					RepositoryID: repo.ID, InstallationID: installation.ID, FullName: repo.FullName,
				})
			}
			if len(response.Repositories) < 100 {
				break
			}
		}
	}
	return user.ID, grants, nil
}
