# Security policy

## Reporting a vulnerability

Please **do not** open a public issue for a security problem.

Use GitHub's private reporting — [Security → Report a
vulnerability](https://github.com/ZoolCoder/findable/security/advisories/new) — or
email the maintainer listed on the repository profile.

You will get an acknowledgement within a few days. This is a small project with
one maintainer, so please be patient; if a fix takes longer than that, you will
be told where it stands rather than left waiting.

## What is in scope

`findable` reads sites it is pointed at, which may be hostile, and writes two
files (robots.txt and llms.txt) into the `write_dir` its config names. The
interesting failures are about a page making it do something, writing
somewhere it should not, and doing something the operator did not review.

Especially in scope:

* **A write outside `write_dir`, or a read outside `path`.** Both go through
  `os.Root`, so a symlink or a `..` cannot lead out of the directory. A way
  around that is a vulnerability.
* **A crawled site steering the crawler.** The crawler follows links,
  sitemaps and redirects on the target's own host only (at most 5 redirects),
  obeys robots.txt for the `findable` token, stops at `max_pages`, caps every
  body at 5 MB and a gzipped sitemap at 50 MB unpacked. A page that makes it
  fetch another host, run without end, or exhaust memory is in scope.
* **Applying something that was not planned.** A saved plan that applies after
  the world moved, or an action executing that the plan did not list.
* **A `MANUAL` action executing.** Those never have a `Do`.
* **The admin page reachable from elsewhere.** `findable ui` has no
  login; it refuses a non-loopback address, answers 403 to a foreign `Host`
  header (DNS rebinding) and to a cross-site `POST`. A way past any of the
  three is a vulnerability.
* **An agent applying without the operator.** The MCP server's `apply` takes
  only the id of a plan it saved and never a plan body, and a plan that
  deletes goes back to the operator's terminal; the plugin's hook asks before
  an `apply` with `-yes`. A path where an agent applies something the operator
  was not shown is in scope.

## What is not in scope

* **A hostile config file.** A config can name any directory you can write to.
  Review a config you did not write, as you would any script.
* **Anything requiring an attacker to already have your shell.**

## Supported versions

The latest release.

## What you can expect from a fix

A security fix gets a release of its own, notes saying what was exposed and
under what conditions, and a test that fails without the fix.
