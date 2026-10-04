CREATE TABLE IF NOT EXISTS comments (
    id TEXT PRIMARY KEY,
    post_id TEXT NOT NULL REFERENCES posts(id),
    parent_id TEXT,
    author_id TEXT NOT NULL,
    text TEXT NOT NULL CHECK (char_length(text) BETWEEN 1 AND 2000),
    created_at TIMESTAMPTZ NOT NULL,
    UNIQUE (post_id, id),
    FOREIGN KEY (post_id, parent_id) REFERENCES comments(post_id, id)
);

CREATE INDEX IF NOT EXISTS comments_page_idx ON comments (post_id, parent_id, created_at, id);
