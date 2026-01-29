# Identity Verification and Storage

The Underground Node Network uses a decentralized identity verification system to establish trust without a central authority.

## How it works

1. **Verification**: When you connect to the entry point with a new SSH key, you are prompted to verify your identity via a platform like GitHub or GitLab.
2. **Key Matching**: The entry point fetches your public keys from the selected platform and compares them to the key used for your current SSH session.
3. **Caching**: Once verified, the entry point saves your identity on disk so you don't have to verify again.

## Storage Format

Verified identities are stored in the `users/` directory (default: `~/.unn/users/`).

### Filename Hash
The filename is the **SHA256 hash** of your SSH public key + the `.identity` extension. 
- Example: `42450b2fe3826f36d810aeb1406c777fb97f57b9e558ca88a21b0cc2f021fbee.identity`

This hash-based approach ensures:
- **Fast Lookup**: The server can instantly find your identity using only your public key.
- **Privacy**: Usernames are not exposed as filenames.
- **Consistency**: The same key always points to the same identity file regardless of the username you chose for the session.

### File Content
The file contains your platform and username in the format: `platform:username`.
- Example: `github:mevdschee`

## Legacy Files

You may see old files in the `users/` directory that are named after a username (e.g., `maurits`) and contain a raw public key. 

> [!IMPORTANT]
> Files named after usernames are **legacy artifacts** from the old manual registration system. They are no longer used by the current software and can be safely deleted. 

The current system relies entirely on the `.identity` files for verification.
