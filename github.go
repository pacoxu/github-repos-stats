package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/google/go-github/v42/github"
	"github.com/olekukonko/tablewriter"
	"golang.org/x/oauth2"
)

var (
	githubUserName string
	projectsPath   string
)

var reposTitle = "### The DaoCloud open-source repos\n"

func init() {
	flag.StringVar(&githubUserName, "username", "", "github user name")
	flag.StringVar(&projectsPath, "projects-path", "projects.json", "if with stared repos")
}

type Repos struct {
	// things we want to stop referencing
	Spec ReposSpec `json:"spec"`
	// status of our unwanted dependencies
	Status ReposStatus `json:"status"`
}

type ReposSpec struct {
	// repos with descriptions or tags
	Repos map[string]string `json:"repos"`
}
type ReposStatus struct {
	// TODO add all the status of github repos to the status with date
	Repos map[string]string `json:"repos"`
}

func readFile(path string) (string, error) {
	content, err := ioutil.ReadFile(path)
	// Convert []byte to string and print to screen
	return string(content), err
}

func fetchAllRepos(projectsPath string, client *github.Client) ([]*github.Repository, []string) {
	// load repos from json
	repos, err := readFile(projectsPath)
	if err != nil {
		log.Fatalf("Error reading repos file %s: %s", repos, err)
	}
	configFromFile := &Repos{}
	decoder := json.NewDecoder(bytes.NewBuffer([]byte(repos)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(configFromFile); err != nil {
		log.Fatalf("Error reading repos file %s: %s", projectsPath, err)
	}

	var allRepos []*github.Repository
	reposList := configFromFile.Spec.Repos
	log.Println("repos: ", reposList)
	// #TODO #2 Github has a rate limit of 60 per hour, so don't add more than 60 projects now
	var skippedRepos []string
	length := len(reposList)
	for repo, _ := range reposList {
		if len(strings.Split(repo, "/")) == 2 {
			splits := strings.Split(repo, "/")
			// org/reponame is splits
			repository, _, err := client.Repositories.Get(context.Background(), splits[0], splits[1])
			if err != nil {
				// for API rate limit error, sleep for a while
				if strings.Contains(err.Error(), "403 API rate limit") {
					if client == nil {
						log.Printf("WARNING: sleep 3600 seconds for unauthorized github API rate limit 60 per hour: %s ", repo)
						time.Sleep(3600 * time.Second)
					} else {
						// TODO: to get the date after `until` may be better
						log.Printf("WARNING: sleep 60 seconds for authorized github API rate limit 5000 per hour: %s ", repo)
						time.Sleep(60 * time.Second)
					}
				}
				// retry once
				repository, _, err = client.Repositories.Get(context.Background(), splits[0], splits[1])
				if err != nil {
					log.Printf("WARNING: skip repo: %s for error: %s", repo, err)
					skippedRepos = append(skippedRepos, repo)
					continue
				}
			}
			allRepos = append(allRepos, repository)
		} else {
			log.Printf("WARNING: skip repo: %s for error not a github repo", repo)
		}

		// api limit by default 60 times per hour
		if client == nil && length > 60 {
			log.Println("Info: sleep 60 seconds as github API rate limit is 60 per hour for unauthenized user.")
			time.Sleep(60 * time.Second)
		}
		// api limit by default 60 times per hour
		if client != nil && length > 5000 {
			log.Println("Info: sleep 1 seconds as github API rate limit is 5000 per hour for authenized user.")
			time.Sleep(1 * time.Second)
		}
	}
	log.Println("repos: ", reposList)
	return allRepos, skippedRepos
}

func makeMdTable(data [][]string, header []string) string {
	tableString := &strings.Builder{}
	table := tablewriter.NewWriter(tableString)
	table.SetHeader(header)
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.SetColWidth(400)
	table.AppendBulk(data)
	table.Render()
	return tableString.String()
}

type RepoStatus struct {
	Name             string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Lauguage         string
	ForksCount       int
	OpenIssuesCount  int
	StargazersCount  int
	SubscribersCount int
	HTMLURL          string
	//TODO lastRelease
	//TODO License
}

func makeReposString(repos []*github.Repository) string {
	reposData := [][]string{}
	for i, repo := range repos {
		repoWithLink := fmt.Sprintf("[%s](%s)", *repo.Name, *repo.HTMLURL)
		var description string
		if repo.Description == nil {
			description = *repo.Name
		} else {
			description = *repo.Description
		}
		// *description = strings.Replace(*description, "\n", "<br>", -1)
		reposData = append(
			reposData,
			[]string{
				strconv.Itoa(i + 1),
				repoWithLink, strconv.Itoa(*repo.StargazersCount),
				(*repo.UpdatedAt).String()[:10],
				(*repo.CreatedAt).String()[:10],
				strconv.Itoa(*repo.ForksCount),
				description,
			},
		)
	}
	// reposData = append(reposData, []string{"sum", "", "", "", "", strconv.Itoa(total)})
	reposString := makeMdTable(reposData, []string{"ID", "Repo", "Stars", "UpdatedAt", "CreatedAt", "ForksCount", "Descriptions"})
	return reposTitle + reposString + "\n"
}

func main() {
	flag.Parse()
	var client *github.Client
	if token, ok := os.LookupEnv("GITHUB_TOKEN"); ok {
		httpClient := &http.Client{
			Transport: &oauth2.Transport{
				Base:   http.DefaultTransport,
				Source: oauth2.ReuseTokenSource(nil, oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})),
			},
		}
		client = github.NewClient(httpClient)
	} else {
		client = github.NewClient(nil)

	}
	repos, skippedRepos := fetchAllRepos(projectsPath, client)
	// change sort logic here
	sort.Slice(repos[:], func(i, j int) bool {
		return *repos[j].StargazersCount < *repos[i].StargazersCount
	})

	newContentString := makeReposString(repos)
	log.Println("Repos Status: \n", newContentString)
	newContentString += "\n\n"
	newContentString += "#### Skipped repos\n"
	newContentString += strings.Join(skippedRepos, "\n")

	readMeFile := path.Join(os.Getenv("GITHUB_WORKSPACE"), "README.md")
	log.Println("README.md path: ", readMeFile)
	readMeContent, err := ioutil.ReadFile(readMeFile)
	if err != nil {
		panic(err)
	}
	re := regexp.MustCompile(`(?s)(<!--START_SECTION:github_repos-->)(.*)(<!--END_SECTION:github_repos-->)`)

	newContent := []byte(re.ReplaceAllString(string(readMeContent), `$1`+"\n"+newContentString+`$3`))
	err = ioutil.WriteFile(readMeFile, newContent, 0644)
	if err != nil {
		panic(err)
	}
}
