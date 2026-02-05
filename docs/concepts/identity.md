# Identity & Verification

The Underground Node Network uses a decentralized identity system that links standard SSH keys to established developer platforms.

### Overview
- **Zero Central Passwords**: Identity is established entirely via SSH public key ownership.
- **Platform Linking**: Users verify their keys against public profiles (GitHub, GitLab, Codeberg, etc.).
- **Global Usernames**: Verified users claim a unique UNN username that is protected and recognized across the entire network.

### Registration Process
1. **Verification**: The entrypoint fetches your public keys from a chosen platform (e.g., `github.com/username.keys`).
2. **Matching**: It compares these keys against the one you are currently using to connect.
3. **Naming**: Once matched, you choose a UNN username (4-20 alphanumeric characters).
4. **Persistence**: Your key hash, username, and last-seen date are saved to the `users` database.

### Onboarding
For new users, the entrypoint presents a "Registration Form" directly in the terminal before allowing access to the main BBS. This ensures every room-hosting user has a verifiable identity.

### Room Identity & Registration
Rooms have a separate but similar registration lifecycle:
- **Host Key as Identity**: A room node identifies itself using its **SSH Host Key**. This is the primary proof of identity when connecting to the entrypoint.
- **Manual Claiming**: Room names must be manually claimed by an owner via the entrypoint using:
  ```bash
  /register <roomname> <host_key_hash>
  ```
- **Verification**: On every connection, the entrypoint verifies that the room's connecting host key matches the hash authorized by the owner for that name.
- **Last Seen**: The registry stores a "last seen" date for each room.

---
See also: [Room Authentication](../apps/room.md)
