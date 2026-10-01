package metrics

const dailyMetricsTableSQL = `CREATE TABLE IF NOT EXISTS daily_metrics (
    day       TEXT PRIMARY KEY,
    count     INT NOT NULL
);`
