1. Sign in where you bought **{{apex}}**, or where its DNS is managed, and find **DNS**, **DNS records** or **Zone editor**.
2. Add a new record of type **CNAME**.
3. In the name field (sometimes *Host* or *Alias*) enter `{{name}}`. Most panels add {{apex}} themselves: don't type it.
4. In the value field (sometimes *Target*, *Points to* or *Data*) enter `{{value}}`.
5. Save. If the panel complains that `{{name}}` already exists, delete the old record first.
