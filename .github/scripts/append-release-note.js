// Append a channel note to a GitHub release body, once.
//
// Called from actions/github-script. The note is read from the environment and
// the current body is fetched from the API (never interpolated into code), so
// release text can't inject script. Callers run in the shared
// `release-notes-<tag>` concurrency group, so read-modify-write is serialized
// across the apt and Homebrew workflows and one note can't overwrite another.
//
// env: RELEASE_TAG, NOTE_MARKER (unique per channel), NOTE_TEXT
module.exports = async ({ github, context, core }) => {
  const { owner, repo } = context.repo;
  const tag = process.env.RELEASE_TAG;
  const marker = `<!-- quark-release-note:${process.env.NOTE_MARKER} -->`;
  const note = process.env.NOTE_TEXT;

  const { data: release } = await github.rest.repos.getReleaseByTag({ owner, repo, tag });
  const body = release.body || '';
  if (body.includes(marker)) {
    core.info(`Release ${tag} already has the ${process.env.NOTE_MARKER} note.`);
    return;
  }

  await github.rest.repos.updateRelease({
    owner,
    repo,
    release_id: release.id,
    body: `${body}\n\n${marker}\n${note}`,
  });
  core.info(`Added ${process.env.NOTE_MARKER} note to release ${tag}.`);
};
