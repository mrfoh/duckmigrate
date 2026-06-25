ALTER TABLE users ADD COLUMN email VARCHAR;
UPDATE users SET email = name || '@example.com';
