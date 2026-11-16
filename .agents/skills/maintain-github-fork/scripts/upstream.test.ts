import { afterEach, beforeEach, expect, test } from "bun:test";
import { existsSync } from "node:fs";
import { mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";

const SCRIPT = path.join(import.meta.dir, "upstream.ts");
const ENV = {
  ...process.env,
  GIT_CONFIG_GLOBAL: "/dev/null",
  GIT_CONFIG_SYSTEM: "/dev/null",
  GIT_AUTHOR_NAME: "Test",
  GIT_AUTHOR_EMAIL: "test@example.invalid",
  GIT_COMMITTER_NAME: "Test",
  GIT_COMMITTER_EMAIL: "test@example.invalid",
  GIT_EDITOR: "true",
};

let tmp: string;
let up: string;
let fork: string;

function git(cwd: string, ...args: string[]): string {
  const result = Bun.spawnSync(["git", ...args], { cwd, env: ENV });
  if (result.exitCode !== 0) {
    throw new Error(`git ${args.join(" ")}: ${result.stderr.toString()}`);
  }
  return result.stdout.toString().trim();
}

function tool(...args: string[]): {
  code: number;
  stdout: string;
  stderr: string;
} {
  const result = Bun.spawnSync([process.execPath, SCRIPT, ...args], {
    cwd: fork,
    env: ENV,
  });
  return {
    code: result.exitCode,
    stdout: result.stdout.toString().trim(),
    stderr: result.stderr.toString().trim(),
  };
}

// Writes the files, commits them, and returns the new commit.
async function commit(
  repo: string,
  subject: string,
  files: Record<string, string>,
  ...bodies: string[]
): Promise<string> {
  for (const [name, content] of Object.entries(files)) {
    await mkdir(path.dirname(path.join(repo, name)), { recursive: true });
    await writeFile(path.join(repo, name), content);
  }
  git(repo, "add", "--all");
  git(
    repo,
    "commit",
    "--quiet",
    "--allow-empty",
    "-m",
    subject,
    ...bodies.flatMap((body) => ["-m", body]),
  );
  return git(repo, "rev-parse", "HEAD");
}

function pendingRows(): string[][] {
  const { stdout } = tool("pending");
  return stdout === "" ? [] : stdout.split("\n").map((line) => line.split("\t"));
}

beforeEach(async () => {
  tmp = await mkdtemp(path.join(tmpdir(), "fork-upstream-"));
  up = path.join(tmp, "up");
  fork = path.join(tmp, "fork");
  await mkdir(up);
  git(up, "init", "--quiet", "--initial-branch=main");
  await commit(up, "base", { "shared.txt": "base\n" });
  git(tmp, "clone", "--quiet", up, fork);
});

afterEach(async () => {
  await rm(tmp, { recursive: true, force: true });
});

test("fetch reads the recorded upstream and follows its branch", async () => {
  expect(tool("fetch").stderr).toContain(".fork/upstream is missing");
  expect(tool("status").stderr).toContain("no upstream fetched yet");

  expect(tool("init", up, "main").code).toBe(0);
  expect(await readFile(path.join(fork, ".fork/upstream"), "utf8")).toBe(
    `${up} main\n`,
  );
  const added = await commit(up, "add a", { "a.txt": "a\n" });
  expect(tool("fetch").stdout).toContain(`tip=${added.slice(0, 7)}`);
  expect(git(fork, "rev-parse", "refs/fork/upstream")).toBe(added);
  expect(tool("status").stdout).toContain("pending=1 unrecorded-local=0");
});

test("pending lists only upstream commits the fork has not accounted for", async () => {
  const merged = await commit(up, "merged", { "merged.txt": "1\n" });
  const equivalent = await commit(up, "equivalent", { "equivalent.txt": "1\n" });
  const trailer = await commit(up, "trailer", { "trailer.txt": "1\n" });
  const picked = await commit(up, "picked", { "picked.txt": "1\n" });
  const decided = await commit(up, "decided", { "decided.txt": "1\n" });
  await commit(up, "android only", { "android/app/Main.kt": "1\n" });
  const mixed = await commit(up, "android and core", {
    "android/app/Other.kt": "1\n",
    "src/core/engine.c": "1\n",
  });
  const overlapping = await commit(up, "touches local work", {
    "src/feature/local.c": "upstream\n",
    "src/feature/new.c": "1\n",
  });
  tool("fetch", up, "main");

  await commit(fork, "local feature", { "src/feature/local.c": "local\n" });
  git(fork, "merge", "--quiet", "--no-edit", merged);
  git(fork, "cherry-pick", equivalent);
  await commit(fork, "port trailer", {}, `Upstream-Commit: ${trailer}`);
  await commit(fork, "port picked", {}, `(cherry picked from commit ${picked})`);
  expect(tool("mark", decided.slice(0, 12), "skipped", "not", "wanted").code).toBe(0);
  expect(tool("mark", "path:android/", "skipped", "no Android build").code).toBe(0);

  expect(pendingRows()).toEqual([
    [mixed, expect.any(String), "android and core", "android/app, src/core", ""],
    [
      overlapping,
      expect.any(String),
      "touches local work",
      "src/feature",
      "src/feature/local.c",
    ],
  ]);
  expect(await readFile(path.join(fork, ".fork/decisions.tsv"), "utf8")).toBe(
    `${decided}\tskipped\tnot wanted\npath:android/\tskipped\tno Android build\n`,
  );
});

test("mark and the decisions file reject entries that cannot be accounted for", async () => {
  const upstream = await commit(up, "upstream", { "a.txt": "a\n" });
  tool("fetch", up, "main");
  const local = await commit(fork, "local", { "local.txt": "1\n" });

  expect(tool("mark", "nonsense", "skipped", "x").stderr).toContain("unknown commit");
  expect(tool("mark", local, "skipped", "x").stderr).toContain("not an upstream commit");
  expect(tool("mark", "path:docs/", "ported", "x").stderr).toContain("can only be skipped");
  expect(tool("mark", upstream, "skipped", "x").code).toBe(0);
  expect(tool("mark", upstream, "ported", "x").stderr).toContain("already recorded");

  await writeFile(path.join(fork, ".fork/decisions.tsv"), `${upstream}\tskipped\n`);
  const broken = tool("pending");
  expect(broken.code).toBe(1);
  expect(broken.stderr).toContain(".fork/decisions.tsv:1");
});

test("local lists the fork's own commits since the record was last committed", async () => {
  const upstream = await commit(up, "upstream", { "a.txt": "a\n" });
  tool("fetch", up, "main");

  const first = await commit(fork, "first feature", { "src/one.c": "1\n" });
  await commit(fork, "port a", { "a.txt": "ported\n" }, `Upstream-Commit: ${upstream}`);
  const subjects = () =>
    tool("local").stdout.split("\n").filter(Boolean).map((line) => line.split("\t")[2]);
  expect(tool("local").stdout.split("\t")[0]).toBe(first);
  expect(subjects()).toEqual(["first feature"]);

  await commit(fork, "record local changes", { ".fork/LOCAL.md": "# Local\n" });
  expect(subjects()).toEqual([]);
  await commit(fork, "second feature", { "src/two.c": "1\n" });
  expect(subjects()).toEqual(["second feature"]);
  expect(tool("status").stdout).toContain("pending=0 unrecorded-local=1");
  expect(
    tool("local", "--all").stdout.split("\n").map((line) => line.split("\t")[2]),
  ).toEqual(["first feature", "second feature"]);
});

test("apply replays clean commits, records empty ones, and hands over conflicts", async () => {
  const clean = await commit(up, "add a", { "a.txt": "a\n" });
  const conflicting = await commit(up, "change shared", { "shared.txt": "upstream\n" });
  const present = await commit(up, "add c", { "c.txt": "c\n" });
  const last = await commit(up, "add d", { "d.txt": "d\n" });
  tool("fetch", up, "main");
  await commit(fork, "local change", {
    "shared.txt": "local\n",
    "c.txt": "c\n",
    "other.txt": "1\n",
    ".fork/decisions.tsv": "# upstream decisions\n",
  });
  git(fork, "switch", "--quiet", "--create", "fork/replay-test");

  const selected = tool("apply", clean, present);
  expect(selected.code).toBe(0);
  expect(selected.stdout.split("\n")).toEqual([
    `applied\t${clean}\tadd a`,
    `empty\t${present}\tadd c`,
  ]);
  expect(git(fork, "log", "-1", "--format=%B")).toContain(
    `(cherry picked from commit ${clean})`,
  );
  expect(await readFile(path.join(fork, ".fork/decisions.tsv"), "utf8")).toContain(
    `\n${present}\tskipped\t`,
  );
  expect(pendingRows().map((row) => row[0])).toEqual([conflicting, last]);

  const stopped = tool("apply");
  expect(stopped.code).toBe(1);
  expect(stopped.stderr).toContain(`stopped at ${conflicting} change shared`);
  expect(existsSync(path.join(fork, ".git/CHERRY_PICK_HEAD"))).toBe(true);
  expect(tool("apply").stderr).toContain("a cherry-pick is in progress");

  await writeFile(path.join(fork, "shared.txt"), "local and upstream\n");
  git(fork, "add", "shared.txt");
  git(fork, "cherry-pick", "--continue");
  const resumed = tool("apply");
  expect(resumed.stdout).toBe(`applied\t${last}\tadd d`);
  expect(pendingRows()).toEqual([]);
  expect(await readFile(path.join(fork, "shared.txt"), "utf8")).toBe(
    "local and upstream\n",
  );
});

test("apply refuses to run outside a replay branch or over uncommitted work", async () => {
  const added = await commit(up, "add a", { "a.txt": "a\n" });
  tool("fetch", up, "main");

  expect(tool("apply").stderr).toContain("not on a replay branch");
  git(fork, "switch", "--quiet", "--create", "fork/replay-test");
  await writeFile(path.join(fork, "shared.txt"), "edited\n");
  const refused = tool("apply");
  expect(refused.code).toBe(1);
  expect(refused.stderr).toContain("uncommitted changes outside .fork/");
  expect(pendingRows().map((row) => row[0])).toEqual([added]);
});
