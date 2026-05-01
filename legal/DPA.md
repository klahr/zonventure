# Data Processing Agreement

> **DRAFT TEMPLATE — NOT LEGAL ADVICE.**
> This is a starting point. Have it reviewed by a lawyer qualified in your jurisdiction before signing or sharing with a customer. Sections most likely to need rework: international transfer mechanism, sub-processor list, audit cadence, security measures (Annex 2).

**Version:** 1.0 — DRAFT
**Last updated:** [DATE]

---

This Data Processing Agreement (this **"DPA"**) supplements the Master Services Agreement (the **"MSA"**) between **Zonventure AB** (organisation number [ZONVENTURE ORG NUMBER], with its registered office at [ZONVENTURE ADDRESS]) (**"Zonventure"** or **"Processor"**) and **[CUSTOMER LEGAL NAME]** (**"Customer"** or **"Controller"**), each a **"Party"** and together the **"Parties"**.

This DPA applies whenever Zonventure processes personal data on Customer's behalf in the Hosted Service, as defined in the MSA. It does not apply to the Self-Hosted Service, where Customer is the controller and processor of its own data.

In the event of a conflict between this DPA and the MSA with respect to the processing of personal data, this DPA prevails.

---

## 1. Definitions

Capitalised terms used but not defined in this DPA have the meanings given in the MSA. The terms **"controller"**, **"processor"**, **"data subject"**, **"personal data"**, **"processing"**, **"personal data breach"**, **"sub-processor"**, **"supervisory authority"**, and **"Standard Contractual Clauses"** (or **"SCCs"**) have the meanings given in Regulation (EU) 2016/679 (the **"GDPR"**).

**"Data Protection Laws"** means all laws applicable to the processing of personal data under this DPA, including the GDPR, the UK Data Protection Act 2018 and UK GDPR, and the Swedish Data Protection Act (Lag (2018:218) med kompletterande bestämmelser till EU:s dataskyddsförordning).

**"Player Data"** has the meaning given in the MSA.

**"Restricted Transfer"** means a transfer of personal data from the European Economic Area, Switzerland, or the United Kingdom to a country that has not been deemed by the relevant authority to provide an adequate level of protection.

## 2. Roles and scope

### 2.1 Roles

The Parties agree that with respect to personal data processed under this DPA:

- **Customer** is the **controller**;
- **Zonventure** is a **processor** acting on behalf of Customer.

### 2.2 Scope of processing

Zonventure will process personal data only on documented instructions from Customer. The MSA, the Documentation, and Customer's configuration of the Service constitute Customer's documented instructions. Customer may issue further instructions in writing.

### 2.3 Compliance with law

Zonventure will inform Customer in writing if Zonventure believes that an instruction infringes Data Protection Laws. Where required by Union or Member State law to process personal data otherwise than on Customer's instructions, Zonventure will inform Customer of that legal requirement before processing, unless that law prohibits such information on important grounds of public interest.

## 3. Description of processing

A description of the subject matter, duration, nature, purpose, types of personal data, and categories of data subjects is set out in **Annex 1**.

## 4. Customer obligations

Customer represents and warrants that:

(a) it has, and will maintain throughout the term, all necessary lawful bases under Data Protection Laws to instruct Zonventure to process personal data as contemplated by this DPA and the MSA;
(b) it has provided all required notices and obtained all required consents from data subjects (including from parents or guardians of any minors), particularly with respect to location data and Player Data;
(c) it has the right to disclose personal data to Zonventure for the purposes of the MSA;
(d) it will use the rights and controls made available by the Service to fulfil its obligations under Data Protection Laws (including in relation to data subject requests).

## 5. Zonventure obligations

### 5.1 Confidentiality

Zonventure will ensure that persons authorised to process personal data are bound by appropriate confidentiality obligations.

### 5.2 Security

Zonventure will implement and maintain the technical and organisational measures set out in **Annex 2** designed to ensure a level of security appropriate to the risk, including measures to protect against unauthorised or unlawful processing and against accidental loss, destruction, damage, alteration, or disclosure.

### 5.3 Sub-processors

Customer authorises Zonventure to engage the sub-processors listed in **Annex 3** for the processing described in Annex 1. Zonventure will:

(a) impose on each sub-processor data-protection obligations no less protective than those in this DPA;
(b) remain liable for the acts and omissions of its sub-processors as if performed by Zonventure;
(c) maintain an up-to-date list of sub-processors at `legal/SUBPROCESSORS.md` (or a customer-accessible equivalent);
(d) provide Customer with at least thirty (30) days' prior notice of any intended addition or replacement of a sub-processor; and
(e) consider in good faith any reasonable objection raised by Customer in writing during that notice period. If the Parties cannot agree, Customer may terminate the affected Order on written notice and receive a pro-rata refund of pre-paid fees for the unused portion of the Subscription Term.

### 5.4 Data subject requests

Taking into account the nature of the processing, Zonventure will assist Customer by appropriate technical and organisational measures, insofar as possible, to fulfil Customer's obligation to respond to requests from data subjects exercising rights under Data Protection Laws. The Service provides:

- self-service data export for Arrangers (Settings → "Download my data");
- self-service "Forget me" controls for Players within `/play`;
- a "Delete account" flow with a 7-day grace window, after which deletion is final;
- API endpoints and admin tools that enable Customer to access, correct, restrict, port, and erase personal data.

Where a data subject contacts Zonventure directly with a request relating to Customer Data, Zonventure will, where lawful, redirect the data subject to Customer and notify Customer.

### 5.5 Assistance

Taking into account the nature of the processing and the information available to Zonventure, Zonventure will assist Customer in ensuring compliance with its obligations under Articles 32–36 GDPR (security, breach notification, data protection impact assessments, and prior consultation) at Customer's reasonable expense.

### 5.6 Personal data breaches

Zonventure will notify Customer without undue delay, and in any event within seventy-two (72) hours, after becoming aware of a personal data breach affecting Customer Data, providing the information required under Article 33(3) GDPR to the extent then known. Zonventure will provide updates as further information becomes available and will reasonably cooperate with Customer's investigation and notification obligations.

### 5.7 Deletion or return

On termination or expiration of the Order to which the personal data relates, and at Customer's election, Zonventure will delete or return all Customer personal data and delete existing copies, except where Union or Member State law requires retention. Zonventure will make Customer Data from the Hosted Service available for export in a structured format on Customer's written request received within thirty (30) days of termination, after which Zonventure will delete Customer Data in accordance with its retention schedule (default: 30 days for location pings post-event; 90 days for audit logs).

### 5.8 Audit

Zonventure will make available to Customer all information necessary to demonstrate compliance with Article 28 GDPR, and will allow for and contribute to audits, including inspections, conducted by Customer or another auditor mandated by Customer. The Parties agree that this obligation is met by Zonventure providing on reasonable written request:

(a) a current copy of `legal/SECURITY.md` and the sub-processor list;
(b) responses to a reasonable number of written security questionnaires per calendar year;
(c) on at least thirty (30) days' written notice and not more than once per calendar year (except where required by a regulator or in response to a personal data breach), a remote audit conducted during normal business hours, in a manner that does not unreasonably interfere with Zonventure's operations and that is subject to confidentiality obligations no less protective than the MSA.

Customer will bear its own audit costs and Zonventure's reasonable out-of-pocket expenses for audit cooperation.

## 6. International transfers

To the extent that Zonventure processes personal data in a country outside the European Economic Area, Switzerland, or the United Kingdom that is not subject to an adequacy decision under Data Protection Laws, the Parties will rely on appropriate safeguards under Article 46 GDPR. The Parties agree that:

(a) the EU Standard Contractual Clauses (Module 2: Controller-to-Processor) approved by Commission Implementing Decision (EU) 2021/914, as applicable, are incorporated by reference into this DPA and apply to such transfers, with the optional clauses selected as set out in Annex 4;
(b) for transfers from the United Kingdom, the UK International Data Transfer Addendum issued by the UK Information Commissioner under section 119A Data Protection Act 2018 applies, completed using the information in this DPA and the Annexes;
(c) for transfers from Switzerland, the SCCs apply with the modifications set out in the FDPIC's communication of 27 August 2021.

The list of countries from which Zonventure may process personal data is set out in **Annex 3**. Zonventure will conduct a transfer impact assessment for each Restricted Transfer and make it available to Customer on reasonable request.

## 7. Liability

Each Party's liability arising under this DPA, whether in contract, tort, or otherwise, is subject to the limitation of liability provisions in the MSA. Nothing in this DPA limits or excludes either Party's liability to a data subject under the SCCs.

## 8. Term and termination

This DPA is effective from the Effective Date of the MSA and continues for the term of the MSA. The provisions of Sections 5.7 (Deletion or return) and 6 (International transfers) survive termination for so long as Zonventure retains personal data on Customer's behalf.

## 9. Order of precedence

In the event of a conflict between (a) the SCCs, (b) this DPA, and (c) the MSA, the order of precedence is: SCCs, then this DPA, then the MSA, with respect to the processing of personal data.

---

## Annex 1 — Description of processing

### A. Subject matter

The provision of the Hosted Service as described in the MSA.

### B. Duration

The duration of the MSA, plus any retention period required under Section 5.7 of this DPA.

### C. Nature and purpose

Processing necessary to operate the Service, including: hosting Adventures, authenticating Arrangers, capturing and displaying Player progress (including location pings during an event), evaluating answers, generating recap and analytics outputs, sending operational email, and meeting security and audit obligations.

### D. Categories of data subjects

- Arrangers (Customer's authorised users of the Service);
- Players (individuals taking part in an Adventure operated by Customer);
- Other Customer personnel who interact with the Service (e.g., billing contacts).

### E. Types of personal data

- **Arranger account data:** email address, organisation name, hashed password, name, IP address and user-agent at login.
- **Player Data during an event:** slot name (chosen by the Arranger or the Player), GPS location pings while consent is active, answer text submitted by the Player, time-stamps of progression through puzzles.
- **Operational data:** login successes/failures, IP addresses, access timestamps, audit-log entries.
- **Email-verification and password-reset tokens:** one-shot, expire in 1 hour, deleted on use.

The Service is not designed to process special categories of personal data under Article 9 GDPR, payment-card data, or government identifiers, and Customer will not submit such data to the Service.

### F. Frequency of processing

Continuous during the Subscription Term.

### G. Period of retention

- Arranger account data: until Customer or the Arranger deletes the account; deletion has a 7-day grace window.
- Player location pings and answers: deleted with the event or after the configured location-retention window (default 30 days post-event), whichever comes first.
- Audit logs: configured retention (default 90 days).
- Verification / reset tokens: 1 hour or until used.

---

## Annex 2 — Technical and organisational measures

A summary of Zonventure's security posture is maintained at `legal/SECURITY.md` and forms part of this DPA. Without limiting that document, Zonventure implements at least the following measures:

### A. Pseudonymisation and encryption

- TLS 1.2+ for all data in transit between client and Service.
- Encryption at rest for the production database and backups.
- Hashed passwords (Argon2id or equivalent modern KDF).
- Magic-link and verification tokens are single-use, time-limited, and stored hashed where applicable.

### B. Confidentiality, integrity, availability, resilience

- Role-based access to production data, restricted to personnel with a documented need.
- Multi-factor authentication required for administrative access to production systems.
- Audit logging of administrative actions on production systems.
- Network segmentation between application, database, and operational tiers.
- Off-site backups with documented restore procedures and periodic restore tests.

### C. Restoration of availability after incident

- Documented backup and restore procedures.
- Disaster-recovery objectives published in `legal/SECURITY.md`.

### D. Regular testing, assessment, and evaluation

- Internal security review at least annually.
- Vulnerability monitoring of dependencies.
- Code review for changes to authentication, authorisation, and data-handling paths.

### E. Personnel

- Personnel with access to personal data are bound by written confidentiality obligations.
- Onboarding and offboarding procedures with timely revocation of access.

### F. Incident management

- Incident response procedures, including the breach-notification path described in Section 5.6.
- Designated security contact: [SECURITY EMAIL].

---

## Annex 3 — Sub-processors

The current list of sub-processors authorised under Section 5.3 is maintained at `legal/SUBPROCESSORS.md` and is incorporated into this DPA by reference. As of the Effective Date, the categories of sub-processors are:

- **Hosting / infrastructure:** [HOSTING PROVIDER NAME], [HOSTING REGION].
- **Email delivery:** [EMAIL PROVIDER NAME], [EMAIL PROVIDER REGION].
- **Operational support tools** (e.g., error monitoring, logging, customer support): see `legal/SUBPROCESSORS.md`.
- **Payment processing** (when applicable): [PAYMENT PROCESSOR NAME].

Each sub-processor processes only the categories of personal data necessary to provide its specific service, as described in `legal/SUBPROCESSORS.md`.

---

## Annex 4 — Standard Contractual Clauses elections

Where Module 2 (Controller-to-Processor) of the SCCs applies under Section 6:

- **Clause 7 (Docking clause):** does not apply.
- **Clause 9 (Use of sub-processors):** Option 2 — General written authorisation. Time period for advance notice of changes: thirty (30) days.
- **Clause 11(a) (Redress):** the optional independent dispute resolution body language does not apply.
- **Clause 17 (Governing law):** the law of Sweden.
- **Clause 18 (Choice of forum and jurisdiction):** the courts of Stockholm, Sweden.

The information required by Annex I.A (List of Parties) is taken from the preamble of this DPA. The information required by Annex I.B (Description of transfer) is set out in Annex 1 above. The information required by Annex II (Technical and organisational measures) is set out in Annex 2 above. The information required by Annex III (List of sub-processors) is set out in Annex 3 above.

---

## Signatures

**Zonventure AB**

Name: ____________________________
Title: ____________________________
Date: ____________________________
Signature: ____________________________

**[CUSTOMER LEGAL NAME]**

Name: ____________________________
Title: ____________________________
Date: ____________________________
Signature: ____________________________

---

## Changelog

- **1.0 — DRAFT** ([DATE]): initial template.
