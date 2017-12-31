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
	"os/exec"
	"time"
)

const (
	UNREAD            = "UNREAD"
	MUST_SAVE         = "save"
	CATEGORY_PERSONAL = "CATEGORY_PERSONAL"
	SUBJECT           = "Subject"
	FROM              = "From"
	MUST_PRINT        = "print"
	PRINTER_NAME      = "Deskjet-3050A-J611-series"
	FILES_PATH        = "../files/"
	CONFIG_PATH       = "../config/"
)

type User struct {
	Name   string   `json:"name"`
	Emails []string `json:"emails"`
}

type Token struct {
	Content string `json:"token"`
}

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

func checkLabel(label string, labels []string) bool {
	for _, lab := range labels {
		if label == lab {
			return true
		}
	}

	return false
}

func isAuthorized(users []User, token string, message *gmail.Message) bool {
	for _, user := range users {
		for _, email := range user.Emails {
			if checkAttribute(message, email, FROM) {
				return true
			}

		}
	}

	// if token is not find the user is definitively unauthorized
	return checkAttribute(message, token, SUBJECT)
}

func request(message *gmail.Message, subjectSnippet string) bool {
	return checkAttribute(message, subjectSnippet, SUBJECT)
}

func checkAttribute(message *gmail.Message, strResearch string, attribute string) bool {
	for _, v := range message.Payload.Headers {
		if v.Name == attribute {
			if len(strings.Split(strings.ToLower(v.Value), strResearch)) > 1 {
				return true
			}
		}
	}

	return false
}

func createFile(stringDecode []byte, filename string) {
	f, err := os.Create(FILES_PATH + filename)
	defer f.Close()
	if err != nil {
		log.Fatalf("Unable to create file. %v", err)
	}

	_, err = f.Write(stringDecode)
	if err != nil {
		log.Fatalf("Unable to write file. %v", err)
	}

	fmt.Println(" => is saved")
}

func removeLabel(label string, srv *gmail.Service, user string, messageId string) {
	modifyMessageRequest := gmail.ModifyMessageRequest{
		nil,
		[]string{label},
		nil,
		nil,
	}
	_, err := srv.Users.Messages.Modify(user, messageId, &modifyMessageRequest).Do()
	if err != nil {
		log.Fatalf("Unable to remove label: %v", err)
	}
}

func saveAttachments(fullMessage *gmail.Message, srv *gmail.Service, user string) *[]string {
	defer removeLabel(UNREAD, srv, user, fullMessage.Id)

	fmt.Println(" => Try to saving")

	var attachments []string
	for _, part := range fullMessage.Payload.Parts {
		if len(part.Body.AttachmentId) > 0 {
			filename := part.Filename
			fmt.Println(filename)

			file, err := srv.Users.Messages.Attachments.Get(user, fullMessage.Id, part.Body.AttachmentId).Do()
			if err != nil {
				log.Fatalf("Unable to get attachments: %v", err)
			}

			sDec, err := base64.URLEncoding.DecodeString(file.Data)
			if err != nil {
				log.Fatalf("Unable to decode string: %v", err)
			}

			createFile(sDec, filename)
			attachments = append(attachments, filename)
		}
	}

	return &attachments
}

func printAttachments(fullMessage *gmail.Message, srv *gmail.Service, user string) {
	attachments := saveAttachments(fullMessage, srv, user)

	fmt.Println(" => Try to start printing")

	for _, fileName := range *attachments {
		_, err := exec.Command("lp", "-d", PRINTER_NAME, FILES_PATH+fileName).Output()
		if err != nil {
			log.Panic(err)
		}

		fmt.Println(" => is in queue for printing")
	}

}

func handleMessage(r *gmail.ListMessagesResponse, srv *gmail.Service, user string, authUsers []User, token string) {
	if len(r.Messages) > 0 {
		for _, m := range r.Messages {
			fullMessage, err := srv.Users.Messages.Get(user, m.Id).Do()
			if err != nil {
				log.Fatalf("Unable to get messages: %v", err)
			}

			labels := fullMessage.LabelIds
			if checkLabel(CATEGORY_PERSONAL, labels) {
				if checkLabel(UNREAD, labels) {
					if isAuthorized(authUsers, token, fullMessage) {

						if request(fullMessage, MUST_PRINT) {
							go printAttachments(fullMessage, srv, user)

						} else if request(fullMessage, MUST_SAVE) {
							go saveAttachments(fullMessage, srv, user)
						}
					} else {
						fmt.Println("User is not authorised")
					}
				}
			}
		}
	}
}

func main() {
	ctx := context.Background()

	b, err := ioutil.ReadFile(CONFIG_PATH + "users.json")
	if err != nil {
		log.Fatalf("Unable to read users file: %v", err)
	}

	var authUsers []User
	json.Unmarshal(b, &authUsers)

	b, err = ioutil.ReadFile(CONFIG_PATH + "token.json")
	if err != nil {
		log.Fatalf("Unable to read token file: %v", err)
	}

	var token Token
	json.Unmarshal(b, &token)

	b, err = ioutil.ReadFile(CONFIG_PATH + "client_secret.json")
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

	for {
		listMessages, err := srv.Users.Messages.List(user).Do()
		if err != nil {
			log.Fatalf("Unable to retrieve messages list. %v", err)
		}

		handleMessage(listMessages, srv, user, authUsers, token.Content)

		time.Sleep(60 * time.Minute)
	}
}
