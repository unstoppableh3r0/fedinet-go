import psycopg2

try:
    conn = psycopg2.connect('postgresql://postgres:agileprocessmodel@db.usqvwowvwqyufmrvqgmw.supabase.co:5432/postgres?sslmode=require')
    cur = conn.cursor()
    
    # Get all FKs referencing identities
    cur.execute("""
        SELECT conname, conrelid::regclass 
        FROM pg_constraint 
        WHERE confrelid = 'identities'::regclass
    """)
    
    print("Found constraints:")
    for r in cur.fetchall():
        print(f'{r[0]} on {r[1]}')
        
except Exception as e:
    print(f"Error: {e}")
finally:
    if 'conn' in locals():
        conn.close()
