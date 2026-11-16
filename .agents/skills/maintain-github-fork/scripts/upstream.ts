#!/usr/bin/env bun
// Usage: upstream.ts init <url> <branch>
//        upstream.ts fetch [<url> <branch>]
//        upstream.ts status
//        upstream.ts pending
//        upstream.ts local [--all]
//        upstream.ts mark <commit|path:glob> ported|skipped <note>
//        upstream.ts apply [<commit> ...]
//
// Tracks which upstream commits a fork has taken and replays the rest. An
// upstream commit is accounted for when it is merged into HEAD,
// patch-equivalent to a local commit, named by a full-SHA "Upstream-Commit:"
// trailer or "(cherry picked from commit ...)" line in local history, recorded
// in .fork/decisions.tsv, or changes only paths that a path rule there skips.
//
// apply cherry-picks pending commits (all, or only the listed ones) onto the
// current fork/replay- branch and leaves the first conflict in progress; rerun
// it once that commit is dealt with.

import {
  appendFileSync,
  existsSync,
  mkdirSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import path from "node:path";

const UPSTREAM_REF = "refs/fork/upstream";
const BRANCH_PREFIX = "fork/replay-";
const SOURCE_FILE = ".fork/upstream";
const DECISIONS_FILE = ".fork/decisions.tsv";
const RECORD_DIR = ".fork/";
const SHA = "[0-9a-f]{40}(?:[0-9a-f]{24})?";
const SHA_PATTERN = new RegExp(`^${SHA}$`);
const PROVENANCE_PATTERN = new RegExp(
  `^(?:Upstream-Commit: *(${SHA})|\\(cherry picked from commit (${SHA})\\))$`,
  "gm",
);
const LOCAL_FILES_SHOWN = 5;

class Failure extends Error {}

type Commit = { sha: string; date: string; subject: string; files: string[] };
type Pending = Commit & { local: string[] };
type Decisions = { keys: Set<string>; rules: Bun.Glob[] };

let root = "";

function run(args: string[]): { ok: boolean; stdout: string; stderr: string } {
  const result = Bun.spawnSync(["git", "-c", "core.quotePath=false", ...args], {
    cwd: root || process.cwd(),
  });
  return {
    ok: result.exitCode === 0,
    stdout: result.stdout.toString().trimEnd(),
    stderr: result.stderr.toString().trim(),
  };
}

function git(...args: string[]): string {
  const result = run(args);
  if (!result.ok) {
    throw new Failure(result.stderr || `git ${args[0]} failed`);
  }
  return result.stdout;
}

function readDecisions(): Decisions {
  const decisions: Decisions = { keys: new Set(), rules: [] };
  const file = path.join(root, DECISIONS_FILE);
  if (!existsSync(file)) {
    return decisions;
  }
  readFileSync(file, "utf8")
    .split("\n")
    .forEach((line, index) => {
      if (line === "" || line.startsWith("#")) {
        return;
      }
      const [key, decision, note, ...rest] = line.split("\t");
      const rule = key.startsWith("path:");
      const valid =
        rest.length === 0 &&
        Boolean(note) &&
        (rule
          ? key.length > "path:".length && decision === "skipped"
          : SHA_PATTERN.test(key) &&
            (decision === "ported" || decision === "skipped"));
      if (!valid) {
        throw new Failure(
          `${DECISIONS_FILE}:${index + 1}: expected <full sha>\\t<ported|skipped>\\t<note> or path:<glob>\\tskipped\\t<note>`,
        );
      }
      decisions.keys.add(key);
      if (rule) {
        const glob = key.slice("path:".length);
        decisions.rules.push(
          new Bun.Glob(glob.endsWith("/") ? `${glob}**` : glob),
        );
      }
    });
  return decisions;
}

// Upstream commits that local history names, and the local commits naming them.
function provenance(): { taken: Set<string>; ports: Set<string> } {
  const taken = new Set<string>();
  const ports = new Set<string>();
  const log = git("log", "--format=%x00%H%n%B", `${UPSTREAM_REF}..HEAD`);
  for (const entry of log.split("\0").slice(1)) {
    const newline = entry.indexOf("\n");
    for (const match of entry.slice(newline + 1).matchAll(PROVENANCE_PATTERN)) {
      taken.add(match[1] ?? match[2]);
      ports.add(entry.slice(0, newline));
    }
  }
  return { taken, ports };
}

// Commits on each side of the fork point with no patch-equivalent on the other
// side, parents before children.
function divergence(): { local: Commit[]; upstream: Commit[] } {
  const sides = { local: [] as Commit[], upstream: [] as Commit[] };
  const log = git(
    "log",
    "--reverse",
    "--topo-order",
    "--no-merges",
    "--cherry-pick",
    "--left-right",
    "--date=short",
    "--format=%x00%m%x09%H%x09%ad%x09%s",
    "--name-only",
    `HEAD...${UPSTREAM_REF}`,
  );
  for (const entry of log.split("\0").slice(1)) {
    const [header, ...files] = entry.split("\n");
    const [side, sha, date, ...subject] = header.split("\t");
    sides[side === "<" ? "local" : "upstream"].push({
      sha,
      date,
      subject: subject.join(" "),
      files: files.filter(Boolean),
    });
  }
  return sides;
}

// Unaccounted upstream commits, each with the files it changes that the
// fork's own commits also changed. The fork's own commits are local commits
// that neither port an upstream commit nor only keep records under .fork/.
function survey(): { pending: Pending[]; own: Commit[] } {
  if (!run(["rev-parse", "--verify", "--quiet", UPSTREAM_REF]).ok) {
    throw new Failure("no upstream fetched yet; run fetch <url> <branch>");
  }
  const decisions = readDecisions();
  const { taken, ports } = provenance();
  const { local, upstream } = divergence();
  const own = local
    .filter((commit) => !ports.has(commit.sha))
    .map((commit) => ({
      ...commit,
      files: commit.files.filter((file) => !file.startsWith(RECORD_DIR)),
    }))
    .filter((commit) => commit.files.length > 0);
  const ownFiles = new Set(own.flatMap((commit) => commit.files));
  const pending = upstream
    .filter(
      (commit) =>
        !decisions.keys.has(commit.sha) &&
        !taken.has(commit.sha) &&
        !(
          commit.files.length > 0 &&
          commit.files.every((file) =>
            decisions.rules.some((rule) => rule.match(file)),
          )
        ),
    )
    .map((commit) => ({
      ...commit,
      local: commit.files.filter((file) => ownFiles.has(file)),
    }));
  return { pending, own };
}

// The fork's own commits made since .fork/ was last committed, which is when
// the local record was last brought up to date.
function unrecorded(own: Commit[]): Commit[] {
  const recorded = git("log", "-1", "--format=%H", "--", RECORD_DIR);
  if (recorded === "") {
    return own;
  }
  const since = new Set(git("rev-list", `${recorded}..HEAD`).split("\n"));
  return own.filter((commit) => since.has(commit.sha));
}

function areas(files: string[]): string {
  const seen = new Set(
    files.map((file) => {
      const parts = file.split("/");
      return parts.length > 2 ? `${parts[0]}/${parts[1]}` : file;
    }),
  );
  return [...seen].join(", ");
}

function row(commit: Commit, ...extra: string[]): string {
  return [
    commit.sha,
    commit.date,
    commit.subject,
    areas(commit.files),
    ...extra,
  ].join("\t");
}

function appendDecision(key: string, decision: string, note: string): void {
  const file = path.join(root, DECISIONS_FILE);
  mkdirSync(path.dirname(file), { recursive: true });
  const unterminated =
    existsSync(file) && !/(^|\n)$/.test(readFileSync(file, "utf8"));
  appendFileSync(
    file,
    `${unterminated ? "\n" : ""}${key}\t${decision}\t${note}\n`,
  );
}

function tip(): string {
  return git(
    "log",
    "-1",
    "--date=short",
    "--format=tip=%h date=%ad",
    UPSTREAM_REF,
  );
}

function init(url: string, branch: string): void {
  const file = path.join(root, SOURCE_FILE);
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, `${url} ${branch}\n`);
}

function fetch(args: string[]): void {
  let [url, branch] = args;
  if (args.length === 0) {
    const file = path.join(root, SOURCE_FILE);
    if (!existsSync(file)) {
      throw new Failure(
        `${SOURCE_FILE} is missing; pass <url> <branch> or run init`,
      );
    }
    const source = readFileSync(file, "utf8").trim();
    const split = source.lastIndexOf(" ");
    if (split < 1) {
      throw new Failure(`${SOURCE_FILE}: expected "<url> <branch>"`);
    }
    url = source.slice(0, split);
    branch = source.slice(split + 1);
  }
  git("fetch", "--no-tags", "--", url, `+refs/heads/${branch}:${UPSTREAM_REF}`);
  console.log(tip());
}

function mark(target: string, decision: string, note: string): void {
  if (decision !== "ported" && decision !== "skipped") {
    throw new Failure(`decision must be ported or skipped: ${decision}`);
  }
  if (note === "" || /[\t\n]/.test(note)) {
    throw new Failure("the note must be one non-empty line without tabs");
  }
  let key = target;
  if (target.startsWith("path:")) {
    if (decision !== "skipped" || target === "path:" || /[\t\n]/.test(target)) {
      throw new Failure("a path rule is path:<glob> and can only be skipped");
    }
  } else {
    const resolved = run([
      "rev-parse",
      "--verify",
      "--quiet",
      `${target}^{commit}`,
    ]);
    if (!resolved.ok) {
      throw new Failure(`unknown commit: ${target}`);
    }
    key = resolved.stdout;
    if (!run(["merge-base", "--is-ancestor", key, UPSTREAM_REF]).ok) {
      throw new Failure(`${target} is not an upstream commit`);
    }
  }
  if (readDecisions().keys.has(key)) {
    throw new Failure(`${key} is already recorded in ${DECISIONS_FILE}`);
  }
  appendDecision(key, decision, note);
}

function apply(requested: string[]): void {
  const picking = path.resolve(
    root,
    git("rev-parse", "--git-path", "CHERRY_PICK_HEAD"),
  );
  if (existsSync(picking)) {
    throw new Failure(
      "a cherry-pick is in progress; finish it with git cherry-pick --continue, --skip, or --abort, then rerun apply",
    );
  }
  const wanted = new Set(
    requested.map((commit) => {
      const resolved = run([
        "rev-parse",
        "--verify",
        "--quiet",
        `${commit}^{commit}`,
      ]);
      if (!resolved.ok) {
        throw new Failure(`unknown commit: ${commit}`);
      }
      return resolved.stdout;
    }),
  );
  const todo = survey().pending.filter(
    (commit) => wanted.size === 0 || wanted.has(commit.sha),
  );
  if (todo.length === 0) {
    console.error("upstream: nothing to apply");
    return;
  }

  const dirty = git("status", "--porcelain", "--untracked-files=no")
    .split("\n")
    .filter((line) => line !== "" && !line.slice(3).startsWith(RECORD_DIR));
  if (dirty.length > 0) {
    throw new Failure(
      `uncommitted changes outside ${RECORD_DIR}; commit or move them first:\n${dirty.join("\n")}`,
    );
  }
  if (!git("branch", "--show-current").startsWith(BRANCH_PREFIX)) {
    throw new Failure(
      `not on a replay branch; from the target branch run: git switch -c ${BRANCH_PREFIX}$(date -u +%F)`,
    );
  }

  for (const commit of todo) {
    const pick = run(["cherry-pick", "-x", commit.sha]);
    if (pick.ok) {
      console.log(`applied\t${commit.sha}\t${commit.subject}`);
      continue;
    }
    // A pick that changes nothing stops with a clean index: the change is
    // already in the tree, so record that instead of handing it over.
    const empty =
      existsSync(picking) &&
      run(["diff", "--cached", "--quiet"]).ok &&
      git("ls-files", "--unmerged") === "";
    if (!empty) {
      console.error([pick.stdout, pick.stderr].filter(Boolean).join("\n"));
      throw new Failure(`stopped at ${commit.sha} ${commit.subject}`);
    }
    git("cherry-pick", "--skip");
    appendDecision(
      commit.sha,
      "skipped",
      "empty cherry-pick: the change is already present",
    );
    console.log(`empty\t${commit.sha}\t${commit.subject}`);
  }
}

function main(argv: string[]): void {
  const [command, ...args] = argv;
  const usage = (text: string) => new Failure(`usage: upstream.ts ${text}`);
  const top = run(["rev-parse", "--show-toplevel"]);
  if (!top.ok) {
    throw new Failure("not inside a Git worktree");
  }
  root = top.stdout;

  switch (command) {
    case "init":
      if (args.length !== 2) throw usage("init <url> <branch>");
      init(args[0], args[1]);
      break;
    case "fetch":
      if (args.length !== 0 && args.length !== 2) {
        throw usage("fetch [<url> <branch>]");
      }
      fetch(args);
      break;
    case "status": {
      if (args.length !== 0) throw usage("status");
      const { pending, own } = survey();
      console.log(
        `${tip()} pending=${pending.length} unrecorded-local=${unrecorded(own).length}`,
      );
      break;
    }
    case "pending":
      if (args.length !== 0) throw usage("pending");
      for (const commit of survey().pending) {
        const shown = commit.local.slice(0, LOCAL_FILES_SHOWN);
        const more = commit.local.length - shown.length;
        console.log(
          row(commit, shown.join(", ") + (more > 0 ? ` (+${more} more)` : "")),
        );
      }
      break;
    case "local": {
      if (args.length > 1 || (args.length === 1 && args[0] !== "--all")) {
        throw usage("local [--all]");
      }
      const { own } = survey();
      for (const commit of args.length === 1 ? own : unrecorded(own)) {
        console.log(row(commit));
      }
      break;
    }
    case "mark":
      if (args.length < 3) {
        throw usage("mark <commit|path:glob> ported|skipped <note>");
      }
      mark(args[0], args[1], args.slice(2).join(" "));
      break;
    case "apply":
      apply(args);
      break;
    default:
      throw usage(
        "init|fetch|status|pending|local|mark|apply (see the header of this file)",
      );
  }
}

try {
  main(process.argv.slice(2));
} catch (error) {
  if (!(error instanceof Failure)) {
    throw error;
  }
  console.error(`upstream: ${error.message}`);
  process.exitCode = 1;
}
