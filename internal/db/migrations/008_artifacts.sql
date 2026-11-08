-- Artifacts are documents that bots or the user save for all bots: plans,
-- reviews, reports. author_id is a bot id or 'user'; it has no foreign key,
-- because an artifact outlives the bot that wrote it.
CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  content TEXT NOT NULL,
  links TEXT NOT NULL DEFAULT '[]',
  author_id TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER NOT NULL
) STRICT;
CREATE INDEX artifacts_by_updated ON artifacts(updated_at);

CREATE TABLE artifact_tags (
  artifact_id TEXT NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (artifact_id, tag)
) STRICT;
CREATE INDEX artifact_tags_by_tag ON artifact_tags(tag);
