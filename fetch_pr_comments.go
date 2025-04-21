package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "io/ioutil"
    "log"
    "net/http"
    "os"
    "strconv"
    "strings"
)

type PullRequest struct {
    Number   int     `json:"number"`
    MergedAt *string `json:"merged_at"`
}

type Comment struct {
    User struct {
        Login string `json:"login"`
    } `json:"user"`
    Body      string `json:"body"`
    CreatedAt string `json:"created_at"`
}

func fetchMergedPRs(owner, repo, token string, count int) ([]PullRequest, error) {
    var mergedPRs []PullRequest
    page := 1
    client := &http.Client{}

    for len(mergedPRs) < count {
        req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo), nil)
        if err != nil {
            return nil, err
        }
        q := req.URL.Query()
        q.Add("state", "closed")
        q.Add("sort", "updated")
        q.Add("direction", "desc")
        q.Add("per_page", "100")
        q.Add("page", strconv.Itoa(page))
        req.URL.RawQuery = q.Encode()
        req.Header.Set("Authorization", "token "+token)
        req.Header.Set("Accept", "application/vnd.github.v3+json")

        resp, err := client.Do(req)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
        }

        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            return nil, err
        }

        var prs []PullRequest
        if err := json.Unmarshal(body, &prs); err != nil {
            return nil, err
        }
        if len(prs) == 0 {
            break
        }
        for _, pr := range prs {
            if pr.MergedAt != nil {
                mergedPRs = append(mergedPRs, pr)
                if len(mergedPRs) >= count {
                    break
                }
            }
        }
        page++
    }
    if len(mergedPRs) > count {
        mergedPRs = mergedPRs[:count]
    }
    return mergedPRs, nil
}

func fetchReviewComments(owner, repo string, prNumber int, token string) ([]Comment, error) {
    var comments []Comment
    page := 1
    client := &http.Client{}

    for {
        req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments", owner, repo, prNumber), nil)
        if err != nil {
            return nil, err
        }
        q := req.URL.Query()
        q.Add("per_page", "100")
        q.Add("page", strconv.Itoa(page))
        req.URL.RawQuery = q.Encode()
        req.Header.Set("Authorization", "token "+token)
        req.Header.Set("Accept", "application/vnd.github.v3+json")

        resp, err := client.Do(req)
        if err != nil {
            return nil, err
        }
        defer resp.Body.Close()

        if resp.StatusCode != http.StatusOK {
            return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
        }

        body, err := ioutil.ReadAll(resp.Body)
        if err != nil {
            return nil, err
        }

        var pageComments []Comment
        if err := json.Unmarshal(body, &pageComments); err != nil {
            return nil, err
        }
        if len(pageComments) == 0 {
            break
        }
        comments = append(comments, pageComments...)
        page++
    }
    return comments, nil
}

func saveComments(prNumber int, comments []Comment) error {
    filename := fmt.Sprintf("pr_%d_comments.txt", prNumber)
    f, err := os.Create(filename)
    if err != nil {
        return err
    }
    defer f.Close()

    for _, c := range comments {
        _, err := f.WriteString(fmt.Sprintf("[%s] %s:\n%s\n%s\n", c.CreatedAt, c.User.Login, c.Body, strings.Repeat("-", 40)))
        if err != nil {
            return err
        }
    }
    return nil
}

func main() {
    owner := flag.String("owner", "", "GitHub repository owner")
    repo := flag.String("repo", "", "GitHub repository name")
    tokenFlag := flag.String("token", "", "GitHub access token (or set GITHUB_TOKEN_PR env var)")
    count := flag.Int("count", 10, "Number of latest merged PRs to fetch")
    flag.Parse()

    token := *tokenFlag
    if token == "" {
        token = os.Getenv("GITHUB_TOKEN_PR")
    }
    if token == "" {
        log.Fatal("Error: GitHub token must be provided via --token or GITHUB_TOKEN_PR environment variable")
    }
    if *owner == "" || *repo == "" {
        log.Fatal("Error: --owner and --repo are required")
    }

    prs, err := fetchMergedPRs(*owner, *repo, token, *count)
    if err != nil {
        log.Fatalf("Error fetching merged PRs: %v", err)
    }
    if len(prs) == 0 {
        fmt.Println("No merged PRs found.")
        return
    }
    for _, pr := range prs {
        fmt.Printf("Fetching comments for PR #%d...\n", pr.Number)
        comments, err := fetchReviewComments(*owner, *repo, pr.Number, token)
        if err != nil {
            log.Printf("Error fetching comments for PR #%d: %v", pr.Number, err)
            continue
        }
        if len(comments) > 0 {
            if err := saveComments(pr.Number, comments); err != nil {
                log.Printf("Error saving comments for PR #%d: %v", pr.Number, err)
            } else {
                fmt.Printf("Saved %d comments to pr_%d_comments.txt\n", len(comments), pr.Number)
            }
        } else {
            fmt.Printf("PR #%d has no review comments.\n", pr.Number)
        }
    }
} 