# Portal Navigation Organizer

Optional full-stack service plugin for configuring presentation-only Portal navigation folders. It owns no parallel navigation state: writes create an internal Portal candidate and use Activation CAS, so user Portal WorkingCopy edits remain untouched.

The built-in Workbench page is the default UI Provider. A compatible plugin may replace that page through the signed single-dispatch extension point while reusing the same management API and permissions.
