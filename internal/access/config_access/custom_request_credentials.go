package configaccess

// CUSTOM (cpamc-custom): credentials presented by a request, for the Google-account
// client request log. See CUSTOM_FEATURES.md ("Google-account clients").

import "net/http"

// RequestCredentials lists the credentials a request presents, in the order Authenticate
// tries them: the Authorization bearer token, X-Goog-Api-Key, X-Api-Key, then the key and
// auth_token query parameters.
func RequestCredentials(r *http.Request) []string {
	credentials := []string{
		extractBearerToken(r.Header.Get("Authorization")),
		r.Header.Get("X-Goog-Api-Key"),
		r.Header.Get("X-Api-Key"),
	}
	if r.URL != nil {
		query := r.URL.Query()
		credentials = append(credentials, query.Get("key"), query.Get("auth_token"))
	}
	return credentials
}
