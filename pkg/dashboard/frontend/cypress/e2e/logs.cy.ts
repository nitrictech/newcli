describe('logs test suite', () => {
  beforeEach(() => {
    cy.viewport('macbook-16')

    cy.visit('/logs')

    cy.wait(500)
  })

  it(`Should create logs for started service`, () => {
    const expectedServices = [
      'services/my-test-secret.ts',
      'services/my-test-service.ts',
      'services/my-test-db.ts',
    ]

    cy.getTestEl('test-row0-origin').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedServices)
    })
    cy.getTestEl('test-row1-origin').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedServices)
    })
    cy.getTestEl('test-row2-origin').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedServices)
    })
  })

  it(`Should create logs for nodemon`, () => {
    const expectedLogs = [
      '$ nodemon -r dotenv/config services/my-test-secret.ts',
      '$ nodemon -r dotenv/config services/my-test-service.ts',
      '$ nodemon -r dotenv/config services/my-test-db.ts',
    ]

    cy.getTestEl('test-row6-msg').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedLogs)
    })
    cy.getTestEl('test-row7-msg').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedLogs)
    })
    cy.getTestEl('test-row8-msg').should(($el) => {
      expect($el.text()).to.be.oneOf(expectedLogs)
    })
  })

  it(`Should purge logs`, () => {
    cy.getTestEl('logs').children().should('have.length.above', 2)

    cy.intercept('DELETE', '/api/logs').as('purge')

    cy.getTestEl('purge-logs-btn').click()

    cy.wait('@purge')

    cy.getTestEl('logs')
      .children()
      .first()
      .should('have.text', 'No logs available')
  })
})
