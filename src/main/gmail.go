package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"path/filepath"

	"golang.org/x/net/context"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"encoding/base64"
	"strings"
)

const (
	UNREAD            = "UNREAD"
	CATEGORY_PERSONAL = "CATEGORY_PERSONAL"
	SUBJECT           = "Subject"
	MUST_PRINT        = "print"
)

// getClient uses a Context and Config to retrieve a Token
// then generate a Client. It returns the generated Client.
func getClient(ctx context.Context, config *oauth2.Config) *http.Client {
	cacheFile, err := tokenCacheFile()
	if err != nil {
		log.Fatalf("Unable to get path to cached credential file. %v", err)
	}
	tok, err := tokenFromFile(cacheFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(cacheFile, tok)
	}
	return config.Client(ctx, tok)
}

// getTokenFromWeb uses Config to request a Token.
// It returns the retrieved Token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var code string
	if _, err := fmt.Scan(&code); err != nil {
		log.Fatalf("Unable to read authorization code %v", err)
	}

	tok, err := config.Exchange(oauth2.NoContext, code)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web %v", err)
	}
	return tok
}

// tokenCacheFile generates credential file path/filename.
// It returns the generated credential path/filename.
func tokenCacheFile() (string, error) {
	usr, err := user.Current()
	if err != nil {
		return "", err
	}
	tokenCacheDir := filepath.Join(usr.HomeDir, ".credentials")
	os.MkdirAll(tokenCacheDir, 0700)
	return filepath.Join(tokenCacheDir,
		url.QueryEscape("gmail-go-quickstart.json")), err
}

// tokenFromFile retrieves a Token from a given file path.
// It returns the retrieved Token and any read error encountered.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	t := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(t)
	defer f.Close()
	return t, err
}

// saveToken uses a file path to create a file and store the
// token in it.
func saveToken(file string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", file)
	f, err := os.OpenFile(file, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

func findLabel(label string, labels []string) bool {
	for _, lab := range labels {
		if label == lab {
			return true
		}
	}

	return false
}

func mustBePrinted(message *gmail.Message) bool {
	for _, v := range message.Payload.Headers {
		if v.Name == SUBJECT {
			if len(strings.Split(strings.ToLower(v.Value), MUST_PRINT)) > 1 {
				return true
			}
		}
	}

	return false
}

func main() {
	ctx := context.Background()

	b, err := ioutil.ReadFile("client_secret.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved credentials
	// at ~/.credentials/gmail-go-quickstart.json
	config, err := google.ConfigFromJSON(b, gmail.GmailModifyScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(ctx, config)

	srv, err := gmail.New(client)
	if err != nil {
		log.Fatalf("Unable to retrieve gmail Client %v", err)
	}

	user := "me"
	r, err := srv.Users.Messages.List(user).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve labels. %v", err)
	}

	if (len(r.Messages) > 0) {
		for _, m := range r.Messages {
			mFull, err := srv.Users.Messages.Get(user, m.Id).Do()
			if err != nil {
				log.Fatalf("Unable to get messages: %v", err)
			}

			labels := mFull.LabelIds
			if findLabel(UNREAD, labels) && findLabel(CATEGORY_PERSONAL, labels) {
				for _, part := range mFull.Payload.Parts {
					if len(part.Body.AttachmentId) > 0 {
						if mustBePrinted(mFull) {
							filename := part.Filename
							if _, err := os.Stat("../files/" + filename); os.IsNotExist(err) {
								fmt.Println(filename)

								file, err := srv.Users.Messages.Attachments.Get(user, m.Id, part.Body.AttachmentId).Do()
								if err != nil {
									log.Fatalf("Unable to get attachments: %v", err)
								}

								sDec, err := base64.URLEncoding.DecodeString(file.Data)
								if err != nil {
									log.Fatalf("Unable to decode string: %v", err)
								}

								f, err := os.Create("../files/" + filename)
								if err != nil {
									log.Fatalf("Unable to create file. %v", err)
								}

								_, err = f.Write(sDec)
								if err != nil {
									log.Fatalf("Unable to write file. %v", err)
								}

								f.Close()

								fmt.Println(" => saved")
							}
						}
					}
				}
			}
		}
	}
}
