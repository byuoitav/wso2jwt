package authmiddleware

import (
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/byuoitav/authmiddleware/bearertoken"
	ad "github.com/byuoitav/authmiddleware/helpers/activedir"
	"github.com/byuoitav/authmiddleware/wso2jwt"
	"github.com/jessemillar/jsonresp"
	"github.com/shenshouer/cas"
)

// Authenticate is the generalized middleware function
// No CAS check for non-user access
func Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// If the request can pass the standard authentication then continue with the request.
		passed, err := MachineChecks(request)
		if err != nil {
			jsonresp.New(writer, http.StatusBadRequest, err.Error())
			return
		}

		if passed {
			next.ServeHTTP(writer, request)
			return
		}

		jsonresp.New(writer, http.StatusBadRequest, "Not authorized")
	})
}

// AuthenticateUser is the middleware function for user access.
func AuthenticateUser(next http.Handler) http.Handler {
	u, _ := url.Parse("https://cas.byu.edu/cas")
	c := cas.NewClient(&cas.Options{
		URL: u,
	})

	return c.HandleFunc(func(w http.ResponseWriter, r *http.Request) {
		// Run through MachineChecks. If not machine access, it is a user so check their rights.
		passed, err := MachineChecks(r)
		if err != nil {
			jsonresp.New(w, http.StatusBadRequest, err.Error())
			return
		}
		// If it passed the MachineChecks, allow access.
		if passed {
			next.ServeHTTP(w, r)
		}
		// If not, run through user checks with AD
		if !passed {
			if !cas.IsAuthenticated(r) {
				cas.RedirectToLogin(w, r)
				return
			}
			// Compare User Active Directory groups against the General Control Groups.
			control := strings.Split(os.Getenv("GEN_CONTROL_GROUPS"), ", ")
			access := PassGatekeeper(cas.Username(r), control)
			if access {
				next.ServeHTTP(w, r)
			}
			if !access {
				jsonresp.New(w, http.StatusBadRequest, "Not authorized")
			}
		}
	})
}

// Boolean function for the standard automated checks that need to pass for any request.
func MachineChecks(request *http.Request) (bool, error) {
	passed, err := checkLocal()
	if err != nil {
		return passed, err
	}
	if passed {
		return passed, nil
	}

	passed, err = checkBearerToken(request)
	if err != nil {
		return passed, err
	}
	if passed {
		return passed, nil
	}

	passed, err = checkWSO2(request)
	if err != nil {
		return passed, err
	}
	if passed {
		return passed, nil
	}

	return passed, err
}

func checkLocal() (bool, error) {
	log.Printf("Local check starting")

	if len(os.Getenv("LOCAL_ENVIRONMENT")) > 0 {
		log.Printf("Authorized via LOCAL_ENVIRONMENT")
		return true, nil
	}

	log.Printf("Local check finished")
	return false, nil
}

func checkBearerToken(request *http.Request) (bool, error) {
	log.Printf("Bearer token check starting")

	token := request.Header.Get("Authorization") // Get the token if it exists

	if len(token) > 0 { // Proceed if we found a token
		parts := strings.Split(token, " ")

		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return false, errors.New("Bad Authorization header")
		}

		valid, err := bearertoken.CheckToken([]byte(parts[1])) // Validate the existing token
		if err != nil {
			return false, err
		}

		if valid {
			log.Println("Bearer token authorized")
			return true, nil
		}
	}

	log.Printf("Bearer token check finished")
	return false, nil
}

func checkWSO2(request *http.Request) (bool, error) {
	log.Printf("WSO2 check starting")

	token := request.Header.Get("X-jwt-assertion") // Get the token if it exists

	if len(token) > 0 { // Proceed if we found a token
		valid, err := wso2jwt.Validate(token) // Validate the existing token
		if err != nil {
			log.Printf("Invalid WSO2 information")
			return false, err
		}

		if valid {
			log.Printf("WSO2 validated successfully")
			return true, nil
		}
	}

	log.Printf("WSO2 check finished")
	return false, nil
}

// PassGatekeeper is the check for a user's Active Directory groups against some control groups
// to allow access based on the needs for the request.
func PassGatekeeper(user string, control []string) bool {
	ADGroups, err := ad.GetGroupsForUser(user)
	if err != nil {
		log.Printf("Error getting groups for the user: %v", err.Error())
		return false
	}

	for i := range control {
		for j := range ADGroups {
			if control[i] == ADGroups[j] {
				return true
			}
		}
	}
	return false
}
