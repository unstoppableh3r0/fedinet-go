import os
import re

file_path = r"c:\Users\gayat\federated_social_networking\federated-backend\admin.go"

with open(file_path, "r", encoding="utf-8") as f:
    content = f.read()

# Define the new function body
new_func = r"""// UpdateServerName updates the server name and notifies all users
func UpdateServerName(newName, updatedBy string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Update server config
	_, err = tx.Exec(`
		INSERT INTO server_config (key, value, updated_by, updated_at)
		VALUES ('server_name', $1, $2, NOW())
		ON CONFLICT (key) DO UPDATE 
		SET value = $1, updated_by = $2, updated_at = NOW()
	`, newName, updatedBy)

	if err != nil {
		return fmt.Errorf("failed to update server config: %v", err)
	}

	// Notify all users about server name change
	err = NotifyAllUsersInTx(tx, "Server Name Updated",
		fmt.Sprintf("The server name has been changed to: %s. Your username is now username@%s", newName, newName),
		"server_change")

	if err != nil {
		return err
	}

	return tx.Commit()
}"""

# Regex to find the function block
# We look for func UpdateServerName... until the end of the block? 
# Matching balanced braces with regex is hard.
# But we know the current function starts at line 166 and ends around 307.
# We can just look for the signature and finding the matching brace manually.

def find_brace_block(text, start_index):
    open_braces = 0
    found_start = False
    for i in range(start_index, len(text)):
        if text[i] == '{':
            open_braces += 1
            found_start = True
        elif text[i] == '}':
            open_braces -= 1
        
        if found_start and open_braces == 0:
            return i + 1
    return -1

start_sig = "func UpdateServerName(newName, updatedBy string) error {"
start_idx = content.find(start_sig)

if start_idx == -1:
    print("Function signature not found!")
    exit(1)

end_idx = find_brace_block(content, start_idx)

if end_idx == -1:
    print("Could not find end of function block")
    exit(1)

print(f"Replacing block from index {start_idx} to {end_idx}")

new_content = content[:start_idx] + new_func + content[end_idx:]

with open(file_path, "w", encoding="utf-8") as f:
    f.write(new_content)

print("Successfully replaced UpdateServerName function.")
