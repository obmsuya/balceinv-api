WITH resources (resource) AS (
    VALUES ('suppliers'), ('purchases'), ('customers'), ('orders'), ('accounting')
),
actions (action) AS (
    VALUES ('view'), ('create'), ('edit'), ('delete')
)
INSERT INTO permissions (id, resource, action, description)
SELECT
    resource || ':' || action,
    resource,
    action,
    upper(substr(action, 1, 1)) || substr(action, 2) || ' ' || resource
FROM resources
CROSS JOIN actions;
