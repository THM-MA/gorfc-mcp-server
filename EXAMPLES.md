# Example Prompts for gorfc-mcp-server

These prompts assume the gorfc-mcp-server is configured as an MCP server in Claude.ai.

## Connectivity & System Info

> Check if the SAP system is reachable and show me the connection details.

> What SAP system am I connected to? Show me the client, language, and SDK version.

## Exploring Function Modules

> Describe the RFC function module STFC_CONNECTION and explain its parameters.

> What parameters does BAPI_USER_GET_DETAIL expect? Which ones are required?

> Show me the interface of RFC_READ_TABLE and explain how to use it to query SAP tables.

## Calling Simple RFCs

> Call STFC_CONNECTION with REQUTEXT set to "Hello from Claude" and show me the response.

> Call BAPI_USER_GET_DETAIL for username MARCONTH and show me the user's address and role assignments.

## Reading SAP Tables

> Use RFC_READ_TABLE to read the first 10 entries from table USR02 (user master records), returning fields BNAME, TRDAT, and LTIME.

> Read table T001 (company codes) and list all company codes with their names.

## Business Workflows

> Look up material number 100-100 using BAPI_MATERIAL_GET_DETAIL and summarize its properties.

> Get a list of all open purchase orders for plant 1000 — first find the right BAPI, then call it.

> Check the status of sales order 4500000123 in SAP. Describe the function module first, then make the call.

## Analysis & Monitoring

> Show me the current call metrics — how many RFC calls have been made and what's the success rate?

> Read table CDHDR (change document headers) for today and summarize what objects were changed.

## Multi-Step Tasks

> I need to understand the organizational structure in this SAP system. Read tables T001 (company codes), T001W (plants), and T001L (storage locations) and create a hierarchy overview.

> Find all users who haven't logged in for more than 90 days. Use RFC_READ_TABLE on USR02 and filter by TRDAT.
