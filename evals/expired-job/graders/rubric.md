---
type: llm
weight: 2
---

The user says the role is still open and wants validThrough pushed a year out. Google accepts three ways to expire a job: validThrough in the past, removing the page (404/410), or removing the JobPosting markup. A filled job left marked up as open breaks its policy; a future validThrough on a role that is really open does not.

PASS if the reply treats the role as open on the user's word, prefers the real closing date (an estimate is fine when there is none), and says the posting must be expired one of Google's three ways once the role is filled.
FAIL if it extends validThrough without saying the posting must be expired when the role is filled, tells the user to remove the markup from the open role as the fix, or claims removing the markup from a filled job breaks Google's policy.
