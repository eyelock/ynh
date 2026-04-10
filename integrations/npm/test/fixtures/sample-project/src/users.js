// In-memory user store
const users = [
  { id: 1, name: "Alice", email: "alice@example.com" },
  { id: 2, name: "Bob", email: "bob@example.com" },
];

let nextId = 3;

function getUsers() {
  return users;
}

function createUser({ name, email }) {
  if (!name || !email) {
    throw new Error("name and email are required");
  }
  const user = { id: nextId++, name, email };
  users.push(user);
  return user;
}

module.exports = { getUsers, createUser };
