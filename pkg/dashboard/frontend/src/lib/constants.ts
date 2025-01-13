import { getHost } from './utils'

export const STORAGE_API = `http://${getHost()}/api/storage`

export const SQL_API = `http://${getHost()}/api/sql`

export const SECRETS_API = `http://${getHost()}/api/secrets`

export const LOGS_API = `http://${getHost()}/api/logs`

export const TABLE_QUERY = `
SELECT
    tbl.schemaname AS schema_name,
    tbl.tablename AS table_name,
    tbl.quoted_name AS qualified_name,
    tbl.is_table AS is_table,
    jsonb_agg(
        jsonb_build_object(
            'column_name', a.attname,
            'data_type', a.data_type,
            'column_order', a.attnum
        )
    ) AS columns
FROM
    (
        SELECT
            n.nspname AS schemaname,
            c.relname AS tablename,
            (quote_ident(n.nspname) || '.' || quote_ident(c.relname)) AS quoted_name,
            true AS is_table
        FROM
            pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE
            c.relkind = 'r'
            AND n.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
            AND n.nspname NOT LIKE 'pg_temp_%'
            AND n.nspname NOT LIKE 'pg_toast_temp_%'
            AND has_schema_privilege(n.oid, 'USAGE') = true
            AND has_table_privilege(quote_ident(n.nspname) || '.' || quote_ident(c.relname), 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') = true
        UNION ALL
        SELECT
            n.nspname AS schemaname,
            c.relname AS tablename,
            (quote_ident(n.nspname) || '.' || quote_ident(c.relname)) AS quoted_name,
            false AS is_table
        FROM
            pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
        WHERE
            c.relkind IN ('v', 'm')
            AND n.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
            AND n.nspname NOT LIKE 'pg_temp_%'
            AND n.nspname NOT LIKE 'pg_toast_temp_%'
            AND has_schema_privilege(n.oid, 'USAGE') = true
            AND has_table_privilege(quote_ident(n.nspname) || '.' || quote_ident(c.relname), 'SELECT, INSERT, UPDATE, DELETE, TRUNCATE, REFERENCES, TRIGGER') = true
    ) AS tbl
    LEFT JOIN (
        SELECT
            attrelid,
            attname,
            format_type(atttypid, atttypmod) AS data_type,
            attnum,
            attisdropped
        FROM
            pg_attribute
    ) AS a ON (
        a.attrelid = tbl.quoted_name::regclass
        AND a.attnum > 0
        AND NOT a.attisdropped
        AND has_column_privilege(tbl.quoted_name, a.attname, 'SELECT, INSERT, UPDATE, REFERENCES')
    )
GROUP BY
    tbl.schemaname,
    tbl.tablename,
    tbl.quoted_name,
    tbl.is_table;
`
// translate permission names to sdk permission names
export const PERMISSION_TO_SDK_LABELS: Record<string, string> = {
  BucketFileGet: 'Read',
  BucketFileList: 'Read',
  BucketFilePut: 'Write',
  KeyValueStoreRead: 'Set',
  KeyValueStoreWrite: 'Get',
}
