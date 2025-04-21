import fetch from 'node-fetch';
import * as fs from 'fs';
import * as path from 'path';
import yargs from 'yargs';
import { hideBin } from 'yargs/helpers';

interface PullRequest {
  number: number;
  merged_at: string | null;
}

interface User {
  login: string;
}

interface Comment {
  user: User;
  body: string;
  created_at: string;
}

async function fetchMergedPRs(owner: string, repo: string, token: string, count: number): Promise<PullRequest[]> {
  let mergedPRs: PullRequest[] = [];
  let page = 1;

  while (mergedPRs.length < count) {
    const res = await fetch(
      `https://api.github.com/repos/${owner}/${repo}/pulls?state=closed&sort=updated&direction=desc&per_page=100&page=${page}`,
      {
        headers: {
          Authorization: `token ${token}`,
          Accept: 'application/vnd.github.v3+json',
        },
      }
    );
    if (!res.ok) {
      throw new Error(`GitHub API returned status ${res.status}`);
    }
    const prs: PullRequest[] = await res.json();
    if (prs.length === 0) break;
    for (const pr of prs) {
      if (pr.merged_at) {
        mergedPRs.push(pr);
        if (mergedPRs.length >= count) break;
      }
    }
    page++;
  }

  return mergedPRs.slice(0, count);
}

async function fetchReviewComments(
  owner: string,
  repo: string,
  prNumber: number,
  token: string
): Promise<Comment[]> {
  let comments: Comment[] = [];
  let page = 1;

  while (true) {
    const res = await fetch(
      `https://api.github.com/repos/${owner}/${repo}/pulls/${prNumber}/comments?per_page=100&page=${page}`,
      {
        headers: {
          Authorization: `token ${token}`,
          Accept: 'application/vnd.github.v3+json',
        },
      }
    );
    if (!res.ok) {
      throw new Error(`GitHub API returned status ${res.status}`);
    }
    const pageComments: Comment[] = await res.json();
    if (pageComments.length === 0) break;
    comments = comments.concat(pageComments);
    page++;
  }

  return comments;
}

function saveComments(prNumber: number, comments: Comment[]): void {
  const filename = path.join(
    process.cwd(),
    `pr_${prNumber}_comments.txt`
  );
  const lines = comments.map(
    (c) => `[${c.created_at}] ${c.user.login}:
${c.body}
${'-'.repeat(40)}
`
  );
  fs.writeFileSync(filename, lines.join(''), 'utf-8');
}

async function main() {
  const argv = await yargs(hideBin(process.argv))
    .option('owner', {
      type: 'string',
      demandOption: true,
      description: 'GitHub repository owner',
    })
    .option('repo', {
      type: 'string',
      demandOption: true,
      description: 'GitHub repository name',
    })
    .option('token', {
      type: 'string',
      description: 'GitHub access token (or set GITHUB_TOKEN_PR env var)',
    })
    .option('count', {
      type: 'number',
      default: 10,
      description: 'Number of latest merged PRs to fetch',
    })
    .help().argv;

  const token = argv.token || process.env.GITHUB_TOKEN_PR;
  if (!token) {
    console.error('Error: GitHub token must be provided via --token or GITHUB_TOKEN_PR environment variable');
    process.exit(1);
  }

  const prs = await fetchMergedPRs(
    argv.owner as string,
    argv.repo as string,
    token,
    argv.count as number
  );
  if (prs.length === 0) {
    console.log('No merged PRs found.');
    return;
  }

  for (const pr of prs) {
    console.log(`Fetching comments for PR #${pr.number}...`);
    const comments = await fetchReviewComments(
      argv.owner as string,
      argv.repo as string,
      pr.number,
      token
    );
    if (comments.length > 0) {
      saveComments(pr.number, comments);
      console.log(`Saved ${comments.length} comments to pr_${pr.number}_comments.txt`);
    } else {
      console.log(`PR #${pr.number} has no review comments.`);
    }
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});

// tsconfig.json と package.json の設定が必要です。 