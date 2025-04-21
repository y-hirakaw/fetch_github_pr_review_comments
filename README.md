# fetch_github_pr_review_comments
GitHubのマージ済みPRからレビューコメントを取得するスクリプトです。

# Go

```
Goがインストールされていない場合
# brew install go
# go version
インストール済みの場合はここから
# go mod init fetch-pr-comments
# go mod tidy
# go run fetch_pr_comments.go --owner=<OWNER> --repo=<REPO> --token=<TOKEN> --count=5
```