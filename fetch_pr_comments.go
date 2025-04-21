// Package main はGitHubのプルリクエストからレビューコメントを取得し、テキストファイルに保存するツールを提供します。
// このツールを使うことで、特定のリポジトリから最近マージされたPRのレビューコメントを簡単に収集できます。
package main

import (
	"encoding/json" // JSONデータの解析に使用
	"flag"          // コマンドラインフラグの処理に使用
	"fmt"           // フォーマット済み入出力に使用
	"io/ioutil"     // I/O操作のためのユーティリティ関数を提供
	"log"           // ログ記録のためのシンプルなパッケージ
	"net/http"      // HTTPクライアント・サーバーの実装を提供
	"os"            // OSの機能とのインタフェースを提供
	"path/filepath" // ファイルパス操作のユーティリティを提供
	"strconv"       // 文字列と他のデータ型間の変換を行う
	"strings"       // 文字列操作のためのユーティリティ関数を提供
)

// PullRequest はGitHub APIから取得したプルリクエスト情報を格納する構造体です。
// GitHubのAPIレスポンスに合わせてJSONタグが設定されています。
type PullRequest struct {
	Number   int     `json:"number"`    // プルリクエスト番号
	MergedAt *string `json:"merged_at"` // マージされた日時（マージされていない場合はnil）
}

// Comment はGitHub APIから取得したコメント情報を格納する構造体です。
// GitHubのAPIレスポンスに合わせてJSONタグが設定されています。
type Comment struct {
	User struct {
		Login string `json:"login"` // コメントを投稿したユーザー名
	} `json:"user"`
	Body      string `json:"body"`       // コメント本文
	CreatedAt string `json:"created_at"` // コメントが作成された日時
}

// PRComment はプルリクエスト番号とそのコメントを関連付ける構造体です。
// マージモードでコメントを1つのファイルにまとめる際に使用します。
type PRComment struct {
	PRNumber int     // コメントが属するプルリクエスト番号
	Comment  Comment // コメントの詳細情報
}

// fetchMergedPRs は指定されたリポジトリから最近マージされたプルリクエストを取得します。
//
// パラメータ:
//   - owner: リポジトリのオーナー名（ユーザー名または組織名）
//   - repo: リポジトリ名
//   - token: GitHub APIアクセス用のトークン
//   - count: 取得するマージ済みPRの数
//
// 戻り値:
//   - []PullRequest: マージ済みプルリクエストの配列
//   - error: エラーが発生した場合はエラー情報、成功時はnil
func fetchMergedPRs(owner, repo, token string, count int) ([]PullRequest, error) {
	var mergedPRs []PullRequest // マージ済みPRを格納するスライス
	page := 1                   // ページネーション用の初期ページ番号
	client := &http.Client{}    // HTTPリクエスト用のクライアント

	// 指定された数のマージ済みPRを取得するまでループ
	for len(mergedPRs) < count {
		// GitHub API用のリクエストを作成
		req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls", owner, repo), nil)
		if err != nil {
			return nil, err // リクエスト作成に失敗した場合はエラーを返す
		}

		// クエリパラメータを設定
		q := req.URL.Query()
		q.Add("state", "closed")          // クローズ済みPRを取得
		q.Add("sort", "updated")          // 更新日時でソート
		q.Add("direction", "desc")        // 降順（最新順）
		q.Add("per_page", "100")          // 1ページあたり100件取得（GitHub APIの上限）
		q.Add("page", strconv.Itoa(page)) // ページ番号
		req.URL.RawQuery = q.Encode()

		// HTTPヘッダーを設定
		req.Header.Set("Authorization", "token "+token)            // 認証トークン
		req.Header.Set("Accept", "application/vnd.github.v3+json") // GitHub API v3を指定

		// リクエストを送信
		resp, err := client.Do(req)
		if err != nil {
			return nil, err // リクエスト送信に失敗した場合はエラーを返す
		}
		defer resp.Body.Close() // 関数終了時にレスポンスボディをクローズ

		// ステータスコードをチェック
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		// レスポンスボディを読み込み
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		// JSONをデコード
		var prs []PullRequest
		if err := json.Unmarshal(body, &prs); err != nil {
			return nil, err
		}

		// 結果が0件の場合はループを終了（これ以上PRがない）
		if len(prs) == 0 {
			break
		}

		// マージ済みPRのみをフィルタリングして追加
		for _, pr := range prs {
			if pr.MergedAt != nil { // マージ済みPRの判定（MergedAtがnilでない）
				mergedPRs = append(mergedPRs, pr)
				if len(mergedPRs) >= count {
					break // 指定数に達したらループを終了
				}
			}
		}
		page++ // 次のページへ
	}

	// 指定された数よりも多く取得した場合は切り詰め
	if len(mergedPRs) > count {
		mergedPRs = mergedPRs[:count]
	}
	return mergedPRs, nil
}

// fetchReviewComments は指定されたプルリクエストのレビューコメントを取得します。
//
// パラメータ:
//   - owner: リポジトリのオーナー名
//   - repo: リポジトリ名
//   - prNumber: プルリクエスト番号
//   - token: GitHub APIアクセス用のトークン
//
// 戻り値:
//   - []Comment: レビューコメントの配列
//   - error: エラーが発生した場合はエラー情報、成功時はnil
func fetchReviewComments(owner, repo string, prNumber int, token string) ([]Comment, error) {
	var comments []Comment   // コメントを格納するスライス
	page := 1                // ページネーション用の初期ページ番号
	client := &http.Client{} // HTTPリクエスト用のクライアント

	// 全ページのコメントを取得するためのループ
	for {
		// GitHub API用のリクエストを作成
		req, err := http.NewRequest("GET", fmt.Sprintf("https://api.github.com/repos/%s/%s/pulls/%d/comments", owner, repo, prNumber), nil)
		if err != nil {
			return nil, err
		}

		// クエリパラメータを設定
		q := req.URL.Query()
		q.Add("per_page", "100")          // 1ページあたり100件取得
		q.Add("page", strconv.Itoa(page)) // ページ番号
		req.URL.RawQuery = q.Encode()

		// HTTPヘッダーを設定
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github.v3+json")

		// リクエストを送信
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()

		// ステータスコードをチェック
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
		}

		// レスポンスボディを読み込み
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}

		// JSONをデコード
		var pageComments []Comment
		if err := json.Unmarshal(body, &pageComments); err != nil {
			return nil, err
		}

		// 結果が0件の場合はループを終了（これ以上コメントがない）
		if len(pageComments) == 0 {
			break
		}

		// 取得したコメントを結果に追加
		comments = append(comments, pageComments...)
		page++ // 次のページへ
	}
	return comments, nil
}

// saveComments はコメントをテキストファイルに保存します。
// 動作モードによって、PRごとに別ファイルに保存するか、すべてを1つのファイルにまとめるかが変わります。
//
// パラメータ:
//   - owner: リポジトリのオーナー名
//   - repo: リポジトリ名
//   - prNumber: プルリクエスト番号（マージモードでは使用されない）
//   - comments: 保存するコメントの配列（通常モードで使用）
//   - mergeMode: マージモードかどうかのフラグ
//   - allComments: すべてのPRのコメント（マージモードで使用）
//
// 戻り値:
//   - error: エラーが発生した場合はエラー情報、成功時はnil
func saveComments(owner, repo string, prNumber int, comments []Comment, mergeMode bool, allComments []PRComment) error {
	// 保存先ディレクトリを作成
	// comments/owner_repo 形式のディレクトリパスを作成
	saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", owner, repo))
	// ディレクトリが存在しない場合は作成（パーミッション0755）
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %v", err)
	}

	// マージモードの場合は、allCommentsを使用して1つのファイルにすべてのコメントを保存
	if mergeMode && allComments != nil {
		// マージされたコメント用のファイル名
		filename := filepath.Join(saveDir, "all_pr_comments.txt")
		// ファイルを作成（既存の場合は上書き）
		f, err := os.Create(filename)
		if err != nil {
			return err
		}
		defer f.Close() // 関数終了時にファイルをクローズ

		// すべてのコメントを順番に書き込み
		for _, prComment := range allComments {
			c := prComment.Comment
			// "PR #番号 [日時] ユーザー名:\nコメント本文\n区切り線" の形式で書き込み
			_, err := f.WriteString(fmt.Sprintf("PR #%d [%s] %s:\n%s\n%s\n",
				prComment.PRNumber, c.CreatedAt, c.User.Login, c.Body, strings.Repeat("-", 40)))
			if err != nil {
				return err
			}
		}
		return nil
	}

	// 通常モード：個別のファイルに保存
	// pr_番号_comments.txt 形式のファイル名を作成
	filename := filepath.Join(saveDir, fmt.Sprintf("pr_%d_comments.txt", prNumber))
	// ファイルを作成（既存の場合は上書き）
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()

	// 各コメントを順番に書き込み
	for _, c := range comments {
		// "[日時] ユーザー名:\nコメント本文\n区切り線" の形式で書き込み
		_, err := f.WriteString(fmt.Sprintf("[%s] %s:\n%s\n%s\n", c.CreatedAt, c.User.Login, c.Body, strings.Repeat("-", 40)))
		if err != nil {
			return err
		}
	}
	return nil
}

// main はプログラムのエントリーポイントです。
// コマンドライン引数を解析し、指定されたGitHubリポジトリからPRコメントを取得して保存します。
func main() {
	// コマンドラインフラグを定義
	owner := flag.String("owner", "", "GitHub repository owner")                                  // GitHubリポジトリのオーナー（ユーザー名または組織名）
	repo := flag.String("repo", "", "GitHub repository name")                                     // GitHubリポジトリ名
	tokenFlag := flag.String("token", "", "GitHub access token (or set GITHUB_TOKEN_PR env var)") // GitHub APIアクセストークン
	count := flag.Int("count", 10, "Number of latest merged PRs to fetch")                        // 取得するPRの数（デフォルト10）
	mergeMode := flag.Bool("merge", false, "Merge all PR comments into a single file")            // すべてのコメントを1ファイルにまとめるかのフラグ
	flag.Parse()                                                                                  // コマンドライン引数を解析

	// トークンの取得（コマンドラインフラグまたは環境変数から）
	token := *tokenFlag
	if token == "" {
		// フラグで指定がなければ環境変数から取得
		token = os.Getenv("GITHUB_TOKEN_PR")
	}
	// トークンがない場合はエラー終了
	if token == "" {
		log.Fatal("Error: GitHub token must be provided via --token or GITHUB_TOKEN_PR environment variable")
	}
	// 必須パラメータのチェック
	if *owner == "" || *repo == "" {
		log.Fatal("Error: --owner and --repo are required")
	}

	// マージ済みPRを取得
	prs, err := fetchMergedPRs(*owner, *repo, token, *count)
	if err != nil {
		log.Fatalf("Error fetching merged PRs: %v", err)
	}
	// 結果が0件の場合は終了
	if len(prs) == 0 {
		fmt.Println("No merged PRs found.")
		return
	}

	// マージモードの場合は、すべてのコメントを一時的に保存するための変数
	var allComments []PRComment
	totalComments := 0 // コメント総数のカウンター

	// 各PRのコメントを処理
	for _, pr := range prs {
		fmt.Printf("Fetching comments for PR #%d...\n", pr.Number)
		// PRのコメントを取得
		comments, err := fetchReviewComments(*owner, *repo, pr.Number, token)
		if err != nil {
			log.Printf("Error fetching comments for PR #%d: %v", pr.Number, err)
			continue // エラーが発生しても次のPRの処理を続行
		}

		// コメントがある場合の処理
		if len(comments) > 0 {
			if *mergeMode {
				// マージモードの場合、コメントをallCommentsに追加して後でまとめて保存
				for _, comment := range comments {
					allComments = append(allComments, PRComment{
						PRNumber: pr.Number,
						Comment:  comment,
					})
				}
				totalComments += len(comments)
				fmt.Printf("Collected %d comments from PR #%d\n", len(comments), pr.Number)
			} else {
				// 通常モード：PRごとに別ファイルに保存
				if err := saveComments(*owner, *repo, pr.Number, comments, false, nil); err != nil {
					log.Printf("Error saving comments for PR #%d: %v", pr.Number, err)
				} else {
					// 保存先パスを表示
					saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", *owner, *repo))
					saveFile := filepath.Join(saveDir, fmt.Sprintf("pr_%d_comments.txt", pr.Number))
					fmt.Printf("Saved %d comments to %s\n", len(comments), saveFile)
				}
			}
		} else {
			fmt.Printf("PR #%d has no review comments.\n", pr.Number)
		}
	}

	// マージモードで、収集したコメントがある場合は保存
	if *mergeMode && len(allComments) > 0 {
		if err := saveComments(*owner, *repo, 0, nil, true, allComments); err != nil {
			log.Printf("Error saving merged comments: %v", err)
		} else {
			// 保存先パスを表示
			saveDir := filepath.Join("comments", fmt.Sprintf("%s_%s", *owner, *repo))
			saveFile := filepath.Join(saveDir, "all_pr_comments.txt")
			fmt.Printf("Saved all %d comments from %d PRs to %s\n", totalComments, len(prs), saveFile)
		}
	}
}
