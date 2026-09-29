CREATE TABLE permissions (
    id TEXT PRIMARY KEY,
    resource TEXT NOT NULL,
    action TEXT NOT NULL CHECK (action IN ('view', 'create', 'edit', 'delete')),
    description TEXT NOT NULL,
    CONSTRAINT permissions_resource_action_unique UNIQUE (resource, action)
);

WITH resources (resource) AS (
    VALUES ('products'), ('sales'), ('users'), ('roles'), ('shops'), ('stock_movements'), ('discounts'), ('reports'), ('settings'), ('notifications')
),
actions (action) AS (
    VALUES ('view'), ('create'), ('edit'), ('delete')
)
INSERT INTO permissions (id, resource, action, description)
SELECT
    resource || ':' || action,
    resource,
    action,
    upper(substr(action, 1, 1)) || substr(action, 2) || ' ' || replace(resource, '_', ' ')
FROM resources
CROSS JOIN actions;
