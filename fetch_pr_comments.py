#!/usr/bin/env python3
import requests
import os
import argparse


def fetch_merged_prs(owner, repo, token, count):
    """
    指定されたリポジトリから最新のマージ済みPRをcount件取得する
    """
    merged_prs = []
    page = 1
    headers = {
        'Authorization': f'token {token}',
        'Accept': 'application/vnd.github.v3+json'
    }

    while len(merged_prs) < count:
        url = f'https://api.github.com/repos/{owner}/{repo}/pulls'
        params = {
            'state': 'closed',
            'sort': 'updated',
            'direction': 'desc',
            'per_page': 100,
            'page': page
        }
        response = requests.get(url, headers=headers, params=params)
        response.raise_for_status()
        prs = response.json()
        if not prs:
            break
        for pr in prs:
            if pr.get('merged_at'):
                merged_prs.append(pr)
                if len(merged_prs) >= count:
                    break
        page += 1

    return merged_prs[:count]


def fetch_review_comments(owner, repo, pr_number, token):
    """
    指定されたPR番号のレビューコメントを全件取得する
    """
    comments = []
    page = 1
    headers = {
        'Authorization': f'token {token}',
        'Accept': 'application/vnd.github.v3+json'
    }

    while True:
        url = f'https://api.github.com/repos/{owner}/{repo}/pulls/{pr_number}/comments'
        params = {'per_page': 100, 'page': page}
        response = requests.get(url, headers=headers, params=params)
        response.raise_for_status()
        page_comments = response.json()
        if not page_comments:
            break
        comments.extend(page_comments)
        page += 1

    return comments


def save_comments(pr_number, comments):
    """
    取得したコメントをテキストファイルに保存する
    """
    filename = f'pr_{pr_number}_comments.txt'
    with open(filename, 'w', encoding='utf-8') as f:
        for c in comments:
            user = c['user']['login']
            body = c['body']
            created = c['created_at']
            f.write(f'[{created}] {user}:\n{body}\n' + '-'*40 + '\n')


def main():
    parser = argparse.ArgumentParser(description='Fetch review comments for merged pull requests')
    parser.add_argument('--owner', required=True, help='GitHub repository owner')
    parser.add_argument('--repo', required=True, help='GitHub repository name')
    parser.add_argument('--token', required=False, help='GitHub access token (or set GITHUB_TOKEN_PR env var)')
    parser.add_argument('--count', type=int, default=10, help='Number of latest merged PRs to fetch')
    args = parser.parse_args()

    token = args.token or os.getenv('GITHUB_TOKEN_PR')
    if not token:
        print('Error: GitHub token must be provided via --token or GITHUB_TOKEN_PR environment variable')
        exit(1)

    prs = fetch_merged_prs(args.owner, args.repo, token, args.count)
    if not prs:
        print('No merged PRs found.')
        return

    for pr in prs:
        pr_number = pr['number']
        print(f'Fetching comments for PR #{pr_number}...')
        comments = fetch_review_comments(args.owner, args.repo, pr_number, token)
        if comments:
            save_comments(pr_number, comments)
            print(f'Saved {len(comments)} comments to pr_{pr_number}_comments.txt')
        else:
            print(f'PR #{pr_number} has no review comments.')

if __name__ == '__main__':
    main()
