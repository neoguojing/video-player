package rtsp

import (
	"errors"
	"strconv"
	"strings"
)

const (
	defaultRealm    = "SENSETIME"
)

// AuthType for auth type
type AuthType string

const (
	// AuthNone No authentication specified
	AuthNone = ""
	// AuthBasic HTTP 1.0 Basic auth from RFC 1945 (also in RFC 2617)
	AuthBasic = "BASIC"
	// AuthDigest HTTP 1.1 Digest auth from RFC 2617
	AuthDigest = "Digest"
)

// AlgoType for algorithm tye
type AlgoType string

const (
	// AlgoMd5 MD5
	AlgoMd5 = "MD5"
	// AlgoMD5Sess MD5-sess
	AlgoMD5Sess = "MD5-sess"
	// AlogToken token
	AlogToken = "token"
)

// DigestParams Digest params
type DigestParams struct {
	response string
	uri      string
	// Client specified nonce
	cnonce string
	// Server specified nonce
	nonce string
	// Server specified digest algorithm
	algorithm AlgoType
	// Quality of protection, containing the one
	qop string
	// A server-specified string that should be
	// included in authentication responses, not
	// included in the actual digest calculation.
	opaque string
	// The server indicated that the auth was ok,
	// but needs to be redone with a new, non-stale nonce.
	stale string
	// Nonce count, the number of earlier replies
	// where this particular nonce has been used.
	nonceCount int
}

// Authorization RTSP auth info
type Authorization struct {
	// The currently chosen auth type.
	authType AuthType
	// Authentication realm
	realm string
	// user name
	username string
	// The parameters specific to digest authentication.
	digestParams DigestParams
}

func parseAuthorization(val string) (*Authorization, error) {

	val = strings.TrimPrefix(val, " ")
	hdrval := strings.SplitN(val, " ", 2)
	if len(hdrval) < 2 {
		return &Authorization{}, errors.New("unauthorized")
	}

	var authorType, algorithm, username, realm, cnonce, nonce, uri, response string
	var nc int
	authorType = strings.Trim(hdrval[0], " ")
	for _, field := range strings.Split(hdrval[1], ",") {
		field = strings.Trim(field, ", ")
		if keyval := strings.SplitN(field, "=", 2); len(keyval) == 2 {
			key := keyval[0]
			val := strings.Trim(keyval[1], `"`)
			switch key {
			case "username":
				username = val
			case "realm":
				realm = val
			case "nonce":
				nonce = val
			case "cnonce":
				cnonce = val
			case "nc":
				nc, _ = strconv.Atoi(val)
			case "uri":
				uri = val
			case "response":
				response = val
			case "algorithm":
				algorithm = val
			}
		}
	}

	auth := &Authorization{
		authType: AuthType(authorType),
		realm:    realm,
		username: username,
		digestParams: DigestParams{
			cnonce:     cnonce,
			nonce:      nonce,
			nonceCount: nc,
			algorithm:  AlgoType(algorithm),
			uri:        uri,
			response:   response,
		},
	}

	return auth, nil
}

// ComputeDigestResponse compute digest response
func ComputeDigestResponse(method, user, pwd string, auth *Authorization) (response string, err error) {
	if len(method) == 0 || auth == nil {
		return "", errors.New("invalid parameter")
	}

	ai := auth.digestParams
	var ha1 string
	ha1Data := user + ":" + auth.realm + ":" + pwd
	ha1 = getMD5(ha1Data)

	switch ai.algorithm {
	case "", AlgoMd5:
	case AlgoMD5Sess:
		a1Data := ha1 + ":" + ai.nonce + ":" + ai.cnonce
		ha1 = getMD5(a1Data)
	default:
		return "", errors.New("unsupported algorithm")
	}

	switch ai.qop {
	case "auth":
		a2 := method + ":" + ai.uri
		ha2 := getMD5(a2)

		a3 := ha1 + ":" + ai.nonce + ":" + strconv.Itoa(ai.nonceCount) + ":" + ai.cnonce + ":" + ai.qop + ":" + ha2
		response = getMD5(a3)
	case "auth-int":
		a2 := method + ":" + ai.uri + ":" + getMD5("")
		ha2 := getMD5(a2)

		a3 := ha1 + ":" + ai.nonce + ":" + strconv.Itoa(ai.nonceCount) + ":" + ai.cnonce + ":" + ai.qop + ":" + ha2
		response = getMD5(a3)
	default:
		a2 := method + ":" + ai.uri
		ha2 := getMD5(a2)

		a3 := ha1 + ":" + ai.nonce + ":" + ha2
		response = getMD5(a3)
	}

	return response, nil
}
