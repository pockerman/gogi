-- the users with a role on an index, so an index can be shared. The creator of every
-- existing index becomes its owner.
CREATE TABLE gogi_index_members (
    index_id UUID NOT NULL REFERENCES gogi_indexes (id) ON DELETE CASCADE,
    member TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('owner', 'reader')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (index_id, member)
);
CREATE INDEX gogi_index_members_member_idx ON gogi_index_members (member);

INSERT INTO gogi_index_members (index_id, member, role)
SELECT id, owner, 'owner' FROM gogi_indexes;
