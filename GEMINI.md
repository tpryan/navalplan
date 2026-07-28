For this project, if you want to run unit tests, run `make test` in the root (it runs backend and frontend unit tests). To run agent evaluations as well, run `make test-all`. 

Don't doublespace go code.  Keep logically joined sections of code together. 

Do not perform git operations. 

Please run goimports on all go code after you write it.
Run goimports before you test go code. More often than not this will catch many issues.  

You can find a go.mod file for gopls in code/app/backend