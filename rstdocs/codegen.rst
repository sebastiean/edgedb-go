
Codegen
=======
edgeql-go is a tool to generate go functions from edgeql queries. When run
in an EdgeDB project directory (or subdirectory) a \*_edgeql.go source file
will be generated for each \*.edgeql file.  The generated go will have an
edgeqlFileName and edgeqlFileNameJSON function with typed arguments and
return value matching the query's arguments and result shape.


Install
-------

.. code-block:: go

    go install github.com/sebastiean/edgedb-go/cmd/edgeql-go@latest
    
See also `pinning tool dependencies <https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module>`_.


Usage
-----

Typically this process would be run using `go generate <https://go.dev/blog/generate>`_ like this:

.. code-block:: go

    //go:generate edgeql-go
    
