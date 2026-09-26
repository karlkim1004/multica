ALTER TABLE autopilot_run ADD COLUMN delivery_status TEXT NOT NULL DEFAULT 'not_requested' CHECK (delivery_status IN ('not_requested','pending','delivered','suppressed','blocked','failed'));
