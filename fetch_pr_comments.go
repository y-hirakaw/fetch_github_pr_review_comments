package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
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

// PRコメント情報を拡張した構造体
type PRComment struct {
	PRNumber int
	Comment  Comment
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

func saveComments(owner, repo string, prNumber int, comments []Comment, mergeMode bool, allComments []PRComment) error {
	// 保存先ディレクトリを作成
	saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", owner, repo))
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// マージモードの場合は、allCommentsを使用
	if mergeMode && allComments != nil {
		// マージされたコメント用のファイル名
		filename := filepath.Join(saveDir, "all_pr_comments.txt")
		f, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer f.Close()

		// 日付でソートする代わりに、取得順で保存
		for _, prComment := range allComments {
			c := prComment.Comment
			_, err := f.WriteString(fmt.Sprintf("PR #%d [%s] %s:\n%s\n%s\n",
				prComment.PRNumber, c.CreatedAt, c.User.Login, c.Body, strings.Repeat("-", 40)))
			if err != nil {
				return err
			}
		}
		return nil
	}

	// 通常モード：個別のファイルに保存
	filename := filepath.Join(saveDir, fmt.Sprintf("pr_%d_comments.txt", prNumber))
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
	mergeMode := flag.Bool("merge", false, "Merge all PR comments into a single file")
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

	// マージモードの場合は、すべてのコメントを一時的に保存
	var allComments []PRComment
	totalComments := 0

	for _, pr := range prs {
		fmt.Printf("Fetching comments for PR #%d...\n", pr.Number)
		comments, err := fetchReviewComments(*owner, *repo, pr.Number, token)
		if err != nil {
			log.Printf("Error fetching comments for PR #%d: %v", pr.Number, err)
			continue
		}

		if len(comments) > 0 {
			if *mergeMode {
				// マージモードの場合、コメントを収集
				for _, comment := range comments {
					allComments = append(allComments, PRComment{
						PRNumber: pr.Number,
						Comment:  comment,
					})
				}
				totalComments += len(comments)
				fmt.Printf("Collected %d comments from PR #%d\n", len(comments), pr.Number)
			} else {
				// 通常モード：個別に保存
				if err := saveComments(*owner, *repo, pr.Number, comments, false, nil); err != nil {
					log.Printf("Error saving comments for PR #%d: %v", pr.Number, err)
				} else {
					saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", *owner, *repo))
					saveFile := filepath.Join(saveDir, fmt.Sprintf("pr_%d_comments.txt", pr.Number))
					fmt.Printf("Saved %d comments to %s\n", len(comments), saveFile)
				}
			}
		} else {
			fmt.Printf("PR #%d has no review comments.\n", pr.Number)
		}
	}

	// マージモードの場合、最後にすべてのコメントを保存
	if *mergeMode && len(allComments) > 0 {
		if err := saveComments(*owner, *repo, 0, nil, true, allComments); err != nil {
			log.Printf("Error saving merged comments: %v", err)
		} else {
			saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", *owner, *repo))
			saveFile := filepath.Join(saveDir, "all_pr_comments.txt")
			fmt.Printf("Saved all %d comments from %d PRs to %s\n", totalComments, len(prs), saveFile)
		}
	}
}
