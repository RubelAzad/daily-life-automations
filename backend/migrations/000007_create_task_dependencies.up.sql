-- Ownership comes from tasks.user_id; both tasks always belong to the same user.
CREATE TABLE IF NOT EXISTS task_dependencies (
  task_id BIGINT UNSIGNED NOT NULL,
  depends_on_id BIGINT UNSIGNED NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  PRIMARY KEY (task_id, depends_on_id),
  INDEX idx_task_deps_depends_on (depends_on_id),
  CONSTRAINT fk_task_deps_task FOREIGN KEY (task_id) REFERENCES tasks (id) ON DELETE CASCADE,
  CONSTRAINT fk_task_deps_depends_on FOREIGN KEY (depends_on_id) REFERENCES tasks (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
