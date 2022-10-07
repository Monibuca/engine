package util

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"time"
)

func ReturnJson[T any](fetch func() T, tickDur time.Duration, rw http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Accept") == "text/event-stream" {
		sse := NewSSE(rw, r.Context())
		tick := time.NewTicker(tickDur)
		defer tick.Stop()
		for range tick.C {
			if sse.WriteJSON(fetch()) != nil {
				return
			}
		}
	} else if err := json.NewEncoder(rw).Encode(fetch()); err != nil {
		http.Error(rw, err.Error(), http.StatusInternalServerError)
	}
}

// func GetJsonHandler[T any](fetch func() T, tickDur time.Duration) http.HandlerFunc {
// 	return func(rw http.ResponseWriter, r *http.Request) {
// 		if r.URL.Query().Get("json") != "" {
// 			if err := json.NewEncoder(rw).Encode(fetch()); err != nil {
// 				rw.WriteHeader(500)
// 			}
// 			return
// 		}
// 		sse := NewSSE(rw, r.Context())
// 		tick := time.NewTicker(tickDur)
// 		for range tick.C {
// 			if sse.WriteJSON(fetch()) != nil {
// 				tick.Stop()
// 				break
// 			}
// 		}
// 	}
// }

func ListenUDP(address string, networkBuffer int) (*net.UDPConn, error) {
	addr, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	if err = conn.SetReadBuffer(networkBuffer); err != nil {
		return nil, err
	}
	if err = conn.SetWriteBuffer(networkBuffer); err != nil {
		return nil, err
	}
	return conn, err
}

// CORS 加入跨域策略头包含CORP
func CORS(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("Access-Control-Allow-Credentials", "true")
		header.Set("Cross-Origin-Resource-Policy", "cross-origin")
		header.Set("Access-Control-Allow-Headers", "Content-Type,Access-Token")
		origin := r.Header["Origin"]
		if len(origin) == 0 {
			header.Set("Access-Control-Allow-Origin", "*")
		} else {
			header.Set("Access-Control-Allow-Origin", origin[0])
		}
		if next != nil && r.Method != "OPTIONS" {
			next.ServeHTTP(w, r)
		}
	})
}

func BasicAuth(u, p string, next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract the username and password from the request
		// Authorization header. If no Authentication header is present
		// or the header value is invalid, then the 'ok' return value
		// will be false.
		username, password, ok := r.BasicAuth()
		if ok {
			// Calculate SHA-256 hashes for the provided and expected
			// usernames and passwords.
			usernameHash := sha256.Sum256([]byte(username))
			passwordHash := sha256.Sum256([]byte(password))
			expectedUsernameHash := sha256.Sum256([]byte(u))
			expectedPasswordHash := sha256.Sum256([]byte(p))

			// 使用 subtle.ConstantTimeCompare() 进行校验
			// the provided username and password hashes equal the
			// expected username and password hashes. ConstantTimeCompare
			// 如果值相等，则返回1，否则返回0。
			// Importantly, we should to do the work to evaluate both the
			// username and password before checking the return values to
			// 避免泄露信息。
			usernameMatch := (subtle.ConstantTimeCompare(usernameHash[:], expectedUsernameHash[:]) == 1)
			passwordMatch := (subtle.ConstantTimeCompare(passwordHash[:], expectedPasswordHash[:]) == 1)

			// If the username and password are correct, then call
			// the next handler in the chain. Make sure to return
			// afterwards, so that none of the code below is run.
			if usernameMatch && passwordMatch {
				if next != nil {
					next.ServeHTTP(w, r)
				}
				return
			}
		}

		// If the Authentication header is not present, is invalid, or the
		// username or password is wrong, then set a WWW-Authenticate
		// header to inform the client that we expect them to use basic
		// authentication and send a 401 Unauthorized response.
		w.Header().Set("WWW-Authenticate", `Basic realm="restricted", charset="UTF-8"`)
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
	})
}
