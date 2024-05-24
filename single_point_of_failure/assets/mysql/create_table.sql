CREATE TABLE IF NOT EXISTS `Accounts` (
  `id` int(16) NOT NULL,
  `name` varchar(50) CHARACTER SET utf8 DEFAULT NULL,
  `phone` varchar(16) CHARACTER SET utf8 DEFAULT NULL,
  INDEX (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
