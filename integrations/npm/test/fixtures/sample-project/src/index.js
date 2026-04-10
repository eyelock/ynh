// Simple web API — the kind of project that would use @ynh/cli
const http = require("http");
const { getUsers, createUser } = require("./users");

const server = http.createServer((req, res) => {
  if (req.method === "GET" && req.url === "/users") {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify(getUsers()));
    return;
  }

  if (req.method === "POST" && req.url === "/users") {
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => {
      const user = createUser(JSON.parse(body));
      res.writeHead(201, { "Content-Type": "application/json" });
      res.end(JSON.stringify(user));
    });
    return;
  }

  res.writeHead(404);
  res.end("Not Found");
});

if (require.main === module) {
  const port = process.env.PORT || 3001;
  server.listen(port, () => console.log(`Listening on :${port}`));
}

module.exports = { server };
