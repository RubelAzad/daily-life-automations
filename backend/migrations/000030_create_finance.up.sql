-- Personal finance: expenses, income, monthly budgets and recurring bills.
-- Amounts are whole cents (minor units) in the user's currency, so sums
-- are exact. LifeOS only records money; it never makes payments.
ALTER TABLE user_settings
  ADD COLUMN currency CHAR(3) NOT NULL DEFAULT 'USD' AFTER week_starts_on;

-- A recurring bill (rent, electricity, a loan payment). next_due_on moves
-- on each time it is paid. pending_task_id is the "Pay …" task made for the
-- current due date, if any: completing it records the payment.
CREATE TABLE IF NOT EXISTS recurring_bills (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  name VARCHAR(100) NOT NULL,
  amount_cents BIGINT NOT NULL,           -- expected amount
  category VARCHAR(40) NOT NULL,
  frequency VARCHAR(10) NOT NULL,         -- weekly | monthly | quarterly | yearly
  due_day TINYINT UNSIGNED NULL,          -- monthly and longer: day of month (31 = last day in shorter months)
  next_due_on DATE NOT NULL,
  remind_days_before TINYINT UNSIGNED NOT NULL DEFAULT 3,
  active TINYINT(1) NOT NULL DEFAULT 1,
  notes VARCHAR(1000) NULL,
  pending_task_id BIGINT UNSIGNED NULL,
  last_paid_on DATE NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_recurring_bills_user (user_id, next_due_on),
  INDEX idx_recurring_bills_due (active, next_due_on),
  UNIQUE KEY uq_recurring_bills_task (pending_task_id),
  CONSTRAINT fk_recurring_bills_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_recurring_bills_task FOREIGN KEY (pending_task_id) REFERENCES tasks (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS expenses (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  amount_cents BIGINT NOT NULL,
  category VARCHAR(40) NOT NULL,
  description VARCHAR(255) NULL,
  spent_on DATE NOT NULL,
  bill_id BIGINT UNSIGNED NULL,           -- set when it pays a recurring bill
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_expenses_user_date (user_id, spent_on),
  INDEX idx_expenses_user_category (user_id, category, spent_on),
  INDEX idx_expenses_bill (bill_id),
  CONSTRAINT fk_expenses_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE,
  CONSTRAINT fk_expenses_bill FOREIGN KEY (bill_id) REFERENCES recurring_bills (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS incomes (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  amount_cents BIGINT NOT NULL,
  source VARCHAR(100) NOT NULL,           -- e.g. Salary, Freelance
  description VARCHAR(255) NULL,
  received_on DATE NOT NULL,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  INDEX idx_incomes_user_date (user_id, received_on),
  CONSTRAINT fk_incomes_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- A monthly spending limit for one category, or for all spending when
-- category is NULL (category_key makes that unique too). An alert is sent
-- once per month when spending reaches alert_percent, and once at 100%.
CREATE TABLE IF NOT EXISTS budgets (
  id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
  user_id BIGINT UNSIGNED NOT NULL,
  category VARCHAR(40) NULL,
  category_key VARCHAR(40) AS (COALESCE(category, '')) STORED,
  monthly_limit_cents BIGINT NOT NULL,
  alert_percent TINYINT UNSIGNED NOT NULL DEFAULT 80,
  created_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
  updated_at TIMESTAMP(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE KEY uq_budgets_category (user_id, category_key),
  CONSTRAINT fk_budgets_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
