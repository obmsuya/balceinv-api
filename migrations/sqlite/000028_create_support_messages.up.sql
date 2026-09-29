CREATE TABLE support_messages (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    shop_id TEXT NULL,
    user_id TEXT NOT NULL,
    topic TEXT NOT NULL CHECK (topic IN ('question', 'problem', 'billing', 'idea')),
    message TEXT NOT NULL CHECK (length(message) BETWEEN 5 AND 5000),
    contact_email TEXT NULL,
    contact_phone TEXT NULL,
    include_details BOOLEAN NOT NULL DEFAULT TRUE,
    details TEXT NOT NULL DEFAULT '{}',
    screenshot_key TEXT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_error TEXT NULL,
    next_attempt_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT support_messages_needs_contact CHECK (contact_email IS NOT NULL OR contact_phone IS NOT NULL),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

CREATE INDEX idx_support_messages_company_id_user_id_created_at ON support_messages (company_id, user_id, created_at);
CREATE INDEX idx_support_messages_company_id_created_at ON support_messages (company_id, created_at);
CREATE INDEX idx_support_messages_status_next_attempt_at ON support_messages (status, next_attempt_at);
